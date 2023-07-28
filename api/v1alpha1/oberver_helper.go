package v1alpha1

import "fmt"

// Name returns the image name.
func (image *Image) Name() string {
	return fmt.Sprintf("%s:%s", image.ImageRepository, image.ImageTag)
}

func (o *Observer) JaegerEndpoint() string {
	res := "jager:4317"
	if o.Spec.Jaeger != nil {
		res = fmt.Sprintf("%s:4317", o.Spec.Jaeger.Name)
	}
	if len(o.Spec.Agent.Endpoint) != 0 {
		res = o.Spec.Agent.Endpoint
	}
	return res
}

func (image *Image) NameByDefault(input string) string {
	if len(image.Name()) != 0 {
		input = image.Name()
	}
	return input
}
