package eso

// PodSpec holds the secret-consuming parts of a core/v1 PodSpec.
type PodSpec struct {
	Containers       []Container `yaml:"containers"`
	InitContainers   []Container `yaml:"initContainers"`
	Volumes          []Volume    `yaml:"volumes"`
	ImagePullSecrets []struct {
		Name string `yaml:"name"`
	} `yaml:"imagePullSecrets"`
}

// Container holds the secret-consuming parts of a container.
type Container struct {
	Name string `yaml:"name"`
	Env  []struct {
		Name      string `yaml:"name"`
		ValueFrom *struct {
			SecretKeyRef *struct {
				Name string `yaml:"name"`
				Key  string `yaml:"key"`
			} `yaml:"secretKeyRef"`
		} `yaml:"valueFrom"`
	} `yaml:"env"`
	EnvFrom []struct {
		Prefix    string `yaml:"prefix"`
		SecretRef *struct {
			Name string `yaml:"name"`
		} `yaml:"secretRef"`
	} `yaml:"envFrom"`
	VolumeMounts []struct {
		Name      string `yaml:"name"`
		MountPath string `yaml:"mountPath"`
		SubPath   string `yaml:"subPath"`
	} `yaml:"volumeMounts"`
}

// KeyToPath maps a Secret key to a file inside a volume.
type KeyToPath struct {
	Key  string `yaml:"key"`
	Path string `yaml:"path"`
}

// Volume holds the secret-backed volume sources.
type Volume struct {
	Name   string `yaml:"name"`
	Secret *struct {
		SecretName string      `yaml:"secretName"`
		Items      []KeyToPath `yaml:"items"`
	} `yaml:"secret"`
	Projected *struct {
		Sources []struct {
			Secret *struct {
				Name  string      `yaml:"name"`
				Items []KeyToPath `yaml:"items"`
			} `yaml:"secret"`
		} `yaml:"sources"`
	} `yaml:"projected"`
}

type podTemplate struct {
	Spec *PodSpec `yaml:"spec"`
}

// Workload decodes any object that embeds a pod spec: Pod (spec), Deployment,
// StatefulSet, DaemonSet, ReplicaSet, Job, Argo Rollout (spec.template.spec)
// and CronJob (spec.jobTemplate.spec.template.spec).
type Workload struct {
	Spec struct {
		PodSpec     `yaml:",inline"`
		Template    *podTemplate `yaml:"template"`
		JobTemplate *struct {
			Spec struct {
				Template *podTemplate `yaml:"template"`
			} `yaml:"spec"`
		} `yaml:"jobTemplate"`
	} `yaml:"spec"`
}

// PodSpec returns the embedded pod spec, or nil if the object has none.
func (w *Workload) PodSpec() *PodSpec {
	switch {
	case w.Spec.Template != nil && w.Spec.Template.Spec != nil:
		return w.Spec.Template.Spec
	case w.Spec.JobTemplate != nil && w.Spec.JobTemplate.Spec.Template != nil &&
		w.Spec.JobTemplate.Spec.Template.Spec != nil:
		return w.Spec.JobTemplate.Spec.Template.Spec
	case len(w.Spec.Containers) > 0:
		return &w.Spec.PodSpec
	}
	return nil
}

// Ingress holds the TLS secret references of a networking.k8s.io Ingress.
type Ingress struct {
	Spec struct {
		TLS []struct {
			Hosts      []string `yaml:"hosts"`
			SecretName string   `yaml:"secretName"`
		} `yaml:"tls"`
	} `yaml:"spec"`
}
