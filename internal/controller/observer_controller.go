/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	observerv1alpha1 "github.com/observer-io/observer/api/v1alpha1"
)

const (
	ObserverLabelKey    = "observer.io/name"
	FinalizerKey        = "observer.io/finalizer"
	InjectContainerName = "instrumentation"
)

// ObserverReconciler reconciles a Observer object
type ObserverReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ob.observer.io,resources=observers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ob.observer.io,resources=observers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ob.observer.io,resources=observers/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Observer object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *ObserverReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(4).Info("Reconciling", "observer", req.NamespacedName.String())

	obj := observerv1alpha1.Observer{}
	err := r.Client.Get(ctx, req.NamespacedName, &obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{Requeue: true}, err
	}
	observer := obj.DeepCopy()

	if !observer.DeletionTimestamp.IsZero() {
		return r.cleanResources(ctx, observer)
	}

	return r.syncObserver(ctx, observer)
}

func (r *ObserverReconciler) cleanResources(ctx context.Context, observer *observerv1alpha1.Observer) (ctrl.Result, error) {
	if observer.Spec.Jaeger == nil {
		return ctrl.Result{}, nil
	}

	deployList := appv1.DeploymentList{}
	if err := r.Client.List(ctx, &deployList, &client.ListOptions{
		Namespace: observer.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{
			ObserverLabelKey: observer.Name,
		}),
	}); err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	for _, deploy := range deployList.Items {
		if err := r.Client.Delete(ctx, &deploy); err != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	svcList := corev1.ServiceList{}
	if err := r.Client.List(ctx, &svcList, &client.ListOptions{
		Namespace: observer.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{
			ObserverLabelKey: observer.Name,
		}),
	}); err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	for _, svc := range svcList.Items {
		if err := r.Client.Delete(ctx, &svc); err != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}
	return r.removeFinalizer(observer)
}

func (r *ObserverReconciler) syncObserver(ctx context.Context, observer *observerv1alpha1.Observer) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var errs []error
	if err := r.ensureJaeger(ctx, observer); err != nil {
		logger.Error(err, "failed to ensure jaeger")
		errs = append(errs, err)
	}

	if err := r.ensureInjectAgent(ctx, observer); err != nil {
		logger.Error(err, "failed to ensure inject agent")
		errs = append(errs, err)
	}

	newStatus := observer.Status.DeepCopy()
	if err := errors.NewAggregate(errs); err != nil {
		SetReadyUnknownCondition(newStatus, "Error", "observer reconcile error")
		SetErrorCondition(newStatus, "ErrorSeen", err.Error())
	} else {
		SetReadyCondition(newStatus, "Ready", "observer reconcile ready")
		ClearErrorCondition(newStatus)
	}

	if err := r.updateStatusIfNeed(ctx, observer, *newStatus); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	return r.ensureFinalizer(observer)
}

func (r *ObserverReconciler) updateStatusIfNeed(ctx context.Context, observer *observerv1alpha1.Observer, newStatus observerv1alpha1.ObserverStatus) error {
	logger := log.FromContext(ctx)
	if !equality.Semantic.DeepEqual(observer.Status, newStatus) {
		observer.Status = newStatus
		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			updateErr := r.Client.Status().Update(ctx, observer)
			if updateErr == nil {
				return nil
			}
			updated := &observerv1alpha1.Observer{}
			if err := r.Client.Get(context.TODO(), client.ObjectKey{Name: observer.Name}, updated); err == nil {
				observer = updated.DeepCopy()
				observer.Status = newStatus
			} else {
				logger.Error(err, fmt.Sprintf("Failed to create/update observer %s/%s", observer.GetNamespace(), observer.GetName()))
			}
			return updateErr
		})
	}
	return nil
}

func (r *ObserverReconciler) ensureJaeger(ctx context.Context, observer *observerv1alpha1.Observer) error {
	if observer.Spec.Jaeger == nil {
		return nil
	}
	if len(observer.Spec.Jaeger.Name) == 0 {
		observer.Spec.Jaeger.Name = observer.Name
	}
	if len(observer.Spec.Jaeger.Namespace) == 0 {
		observer.Spec.Jaeger.Namespace = observer.Namespace
	}
	if observer.Spec.Jaeger.Labels == nil {
		observer.Spec.Jaeger.Labels = map[string]string{
			ObserverLabelKey: observer.Name,
		}
	} else {
		observer.Spec.Jaeger.Labels[ObserverLabelKey] = observer.Name
	}
	err := r.ensureJaegerDeployment(ctx, observer.Spec.Jaeger)
	if err != nil {
		return err
	}
	return r.ensureJaegerService(ctx, observer.Spec.Jaeger)
}

func (r *ObserverReconciler) ensureJaegerDeployment(ctx context.Context, jaegerConfig *observerv1alpha1.ObserverJaeger) error {
	deploy := newJaegerDeployment(jaegerConfig)
	return r.createOrUpdateDeployment(ctx, deploy)
}

func (r *ObserverReconciler) ensureJaegerService(ctx context.Context, jaegerConfig *observerv1alpha1.ObserverJaeger) error {
	svc := newJaegerService(jaegerConfig)
	return r.createOrUpdateService(ctx, svc)
}

func (r *ObserverReconciler) ensureInjectAgent(ctx context.Context, observer *observerv1alpha1.Observer) error {
	logger := log.FromContext(ctx)
	observer.Spec.Agent.Endpoint = observer.JaegerEndpoint()

	var errs []error
	for _, selector := range observer.Spec.ResourceSelectors {
		selector.Namespace = observer.Namespace
		switch selector.Kind {
		case "Deployment":
			if err := r.injectDeployment(ctx, observer, selector); err != nil {
				logger.Error(err, "failed to inject for deployment")
				errs = append(errs, err)
				continue
			}
		case "StatefulSet":
			if err := r.injectStatefulSet(ctx, observer, selector); err != nil {
				logger.Error(err, "failed to inject for statefulset")
				errs = append(errs, err)
				continue
			}
		default:
			errs = append(errs, fmt.Errorf("unsupport resource kind %s", selector.Kind))
		}
	}
	return errors.NewAggregate(errs)
}

func (r *ObserverReconciler) injectDeployment(ctx context.Context, observer *observerv1alpha1.Observer, selector observerv1alpha1.ResourceSelector) error {
	deploy := appv1.Deployment{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}, &deploy); err != nil {
		return err
	}
	if isInjected(deploy.Spec.Template.Spec.Containers) {
		return nil
	}

	initContainer := getInitContainer(observer.Spec.Launcher)
	agentContainer := getAgentContainer(observer.Spec.Agent, deploy.Name, deploy.Spec.Template.Spec.Containers[0])
	injectedPodTemplate, err := r.injectPodTemplateSpec(deploy.Spec.Template, initContainer, agentContainer)
	if err != nil {
		return err
	}
	deploy.Spec.Template = *injectedPodTemplate
	return r.createOrUpdateDeployment(ctx, &deploy)
}

func (r *ObserverReconciler) injectStatefulSet(ctx context.Context, observer *observerv1alpha1.Observer, selector observerv1alpha1.ResourceSelector) error {
	sts := appv1.StatefulSet{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}, &sts); err != nil {
		return err
	}
	if isInjected(sts.Spec.Template.Spec.Containers) {
		return nil
	}
	initContainer := getInitContainer(observer.Spec.Launcher)
	agentContainer := getAgentContainer(observer.Spec.Agent, sts.Name, sts.Spec.Template.Spec.Containers[0])
	injectedPodTemplate, err := r.injectPodTemplateSpec(sts.Spec.Template, initContainer, agentContainer)
	if err != nil {
		return err
	}
	sts.Spec.Template = *injectedPodTemplate
	return r.createOrUpdateStatefulSet(ctx, &sts)
}

func (r *ObserverReconciler) injectPodTemplateSpec(tmpl corev1.PodTemplateSpec, initContainer corev1.Container, agentContainer corev1.Container) (*corev1.PodTemplateSpec, error) {
	if len(tmpl.Spec.Containers) == 0 {
		return nil, fmt.Errorf("can not found any containers")
	}
	tmpl.Spec.InitContainers = append(tmpl.Spec.InitContainers, initContainer)
	tmpl.Spec.Containers = append(tmpl.Spec.Containers, agentContainer)
	tmpl.Spec.Containers[0].Command = append([]string{"/odigos-launcher/launch"}, tmpl.Spec.Containers[0].Command...)
	tmpl.Spec.Containers[0].VolumeMounts = append(
		tmpl.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{
			Name:      "launcherdir",
			MountPath: "/odigos-launcher",
		})
	tmpl.Spec.Volumes = append(
		tmpl.Spec.Volumes,
		corev1.Volume{
			Name: "launcherdir",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		corev1.Volume{
			Name: "kernel-debug",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/sys/kernel/debug",
				},
			},
		})
	return &tmpl, nil
}

func (r *ObserverReconciler) createOrUpdateDeployment(ctx context.Context, deploy *appv1.Deployment) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		got := appv1.Deployment{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: deploy.Namespace, Name: deploy.Name}, &got)
		if apierrors.IsNotFound(err) {
			if err := r.Client.Create(ctx, deploy); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		deploy.SetResourceVersion(got.GetResourceVersion())
		return r.Client.Update(ctx, deploy)
	})
}

func (r *ObserverReconciler) createOrUpdateStatefulSet(ctx context.Context, sts *appv1.StatefulSet) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		got := appv1.StatefulSet{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: sts.Namespace, Name: sts.Name}, &got)
		if apierrors.IsNotFound(err) {
			if err := r.Client.Create(ctx, sts); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		sts.SetResourceVersion(got.GetResourceVersion())
		return r.Client.Update(ctx, sts)
	})
}

func (r *ObserverReconciler) createOrUpdateService(ctx context.Context, svc *corev1.Service) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		got := corev1.Service{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, &got)
		if apierrors.IsNotFound(err) {
			if err := r.Client.Create(ctx, svc); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		svc.SetResourceVersion(got.GetResourceVersion())
		svc.Spec.ClusterIP = got.Spec.ClusterIP
		return r.Client.Update(ctx, svc)
	})
}

func (r *ObserverReconciler) removeFinalizer(observer *observerv1alpha1.Observer) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(observer, FinalizerKey) {
		return ctrl.Result{}, nil
	}

	controllerutil.RemoveFinalizer(observer, FinalizerKey)
	err := r.Client.Update(context.TODO(), observer)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	return ctrl.Result{}, nil
}

func (r *ObserverReconciler) ensureFinalizer(observer *observerv1alpha1.Observer) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(observer, FinalizerKey) {
		return ctrl.Result{}, nil
	}

	controllerutil.AddFinalizer(observer, FinalizerKey)
	err := r.Client.Update(context.TODO(), observer)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ObserverReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&observerv1alpha1.Observer{}).
		Complete(r)
}

func isInjected(containers []corev1.Container) bool {
	nameSet := sets.Set[string]{}
	for _, container := range containers {
		nameSet.Insert(container.Name)
	}
	return nameSet.Has(InjectContainerName)
}

func newJaegerDeployment(jaegerConfig *observerv1alpha1.ObserverJaeger) *appv1.Deployment {
	return &appv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      jaegerConfig.Name,
			Namespace: jaegerConfig.Namespace,
			Labels:    jaegerConfig.Labels,
		},
		Spec: appv1.DeploymentSpec{
			Replicas: jaegerConfig.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"observer.io/app": jaegerConfig.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"observer.io/app": jaegerConfig.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "opentelemetry-all-in-one",
							Image: jaegerConfig.Image.Name(),
						},
					},
				},
			},
		},
	}
}

func newJaegerService(jaegerConfig *observerv1alpha1.ObserverJaeger) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      jaegerConfig.Name,
			Namespace: jaegerConfig.Namespace,
			Labels:    jaegerConfig.Labels,
		},
		Spec: corev1.ServiceSpec{
			Type: jaegerConfig.ServiceType,
			Selector: map[string]string{
				"observer.io/app": jaegerConfig.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "grpc",
					Port:       4317,
					TargetPort: intstr.Parse("4317"),
				},
				{
					Name:       "ui",
					Port:       16686,
					TargetPort: intstr.Parse("16686"),
				},
			},
		},
	}
}

func getInitContainer(launcherConfig *observerv1alpha1.Launcher) corev1.Container {
	return corev1.Container{
		Name:    "copy-launcher",
		Image:   launcherConfig.NameByDefault("keyval/launcher:v0.1"),
		Command: []string{"cp", "-a", "/kv-launcher/.", "/odigos-launcher/"},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "launcherdir",
				MountPath: "/odigos-launcher",
			},
		},
	}
}

func getAgentContainer(agentConfig *observerv1alpha1.Agent, name string, existContainer corev1.Container) corev1.Container {
	return corev1.Container{
		Name:  InjectContainerName,
		Image: agentConfig.Image.Name(),
		Env: []corev1.EnvVar{
			{
				Name:  "OTEL_TARGET_EXE",
				Value: existContainer.Command[0],
			},
			{
				Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
				Value: agentConfig.Endpoint,
			},
			{
				Name:  "OTEL_SERVICE_NAME",
				Value: name,
			},
		},
	}
}
