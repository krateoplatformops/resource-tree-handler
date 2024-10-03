package secrets

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

func Get(ctx context.Context, rc *rest.Config, sel *SecretKeySelector) (*corev1.Secret, error) {
	cli, err := rest.RESTClientFor(rc)
	if err != nil {
		return nil, err
	}

	res := &corev1.Secret{}
	err = cli.Get().
		Resource("secrets").
		Namespace(sel.Namespace).Name(sel.Name).
		Do(ctx).
		Into(res)

	return res, err
}

func Create(ctx context.Context, rc *rest.Config, secret *corev1.Secret) error {
	cli, err := rest.RESTClientFor(rc)
	if err != nil {
		return err
	}

	return cli.Post().
		Namespace(secret.GetNamespace()).
		Resource("secrets").
		Body(secret).
		Do(ctx).
		Error()
}

func Update(ctx context.Context, rc *rest.Config, secret *corev1.Secret) error {
	cli, err := rest.RESTClientFor(rc)
	if err != nil {
		return err
	}
	return cli.Put().
		Namespace(secret.GetNamespace()).
		Resource("secrets").
		Name(secret.Name).
		Body(secret).
		Do(ctx).
		Error()
}

func Delete(ctx context.Context, rc *rest.Config, sel *SecretKeySelector) error {
	cli, err := rest.RESTClientFor(rc)
	if err != nil {
		return err
	}

	return cli.Delete().
		Namespace(sel.Namespace).
		Resource("secrets").
		Name(sel.Name).
		Do(ctx).
		Error()
}

// A SecretKeySelector is a reference to a secret key in an arbitrary namespace.
type SecretKeySelector struct {
	// Name of the referenced object.
	Name string `json:"name"`

	// Namespace of the referenced object.
	Namespace string `json:"namespace"`

	// The key to select.
	Key string `json:"key"`
}

// DeepCopy copy the receiver, creates a new SecretKeySelector.
func (in *SecretKeySelector) DeepCopy() *SecretKeySelector {
	if in == nil {
		return nil
	}
	out := new(SecretKeySelector)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copy the receiver, writes into out. in must be non-nil.
func (in *SecretKeySelector) DeepCopyInto(out *SecretKeySelector) {
	*out = *in
}
