package apiobjects

// SubjectStatus struct should be inlined into the status field of any Subject, these are the "generic bits."
type SubjectStatus struct {
	// ObservedGeneration reflects the metadata.generation last reconciled, it's a vector clock to let you know when
	// the operator is guaranteed to have affected a certain change.
	ObservedGeneration int64    `json:"observedGeneration,omitempty"`
	Issues             []string `json:"issues,omitempty"`
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubjectStatus) DeepCopyInto(out *SubjectStatus) {
	*out = *in
	if in.Issues != nil {
		in, out := &in.Issues, &out.Issues
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new SubjectStatus.
func (in *SubjectStatus) DeepCopy() *SubjectStatus {
	if in == nil {
		return nil
	}
	out := new(SubjectStatus)
	in.DeepCopyInto(out)
	return out
}

// SecretKeyRef is just like SecretKeySelector but the Key is also optional, and a default key (in a default secret)
// should be filled in by the FillDefaults method.
type SecretKeyRef struct {
	// The name of the secret in the subject's namespace to select from. If unspecified, a default secret may be elected
	// by the reconciler logic.
	Secret string `json:"secret,omitempty"`
	// The key of the secret to select from. Must be a valid secret key. If unspecified, a default key may be provided
	// by the reconciler logic.
	Key string `json:"key,omitempty"`
	// Specify whether the secret and key must be defined, default is secret-dependent. When optional, the resolved value of the
	// secret will be the empty string when either the key or the secret do not exist.
	Optional *bool `json:"optional,omitempty"`
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretKeyRef) DeepCopyInto(out *SecretKeyRef) {
	*out = *in
	if in.Optional != nil {
		in, out := &in.Optional, &out.Optional
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is a  deepcopy function, copying the receiver, creating a new SecretKeyRef.
func (in *SecretKeyRef) DeepCopy() *SecretKeyRef {
	if in == nil {
		return nil
	}
	out := new(SecretKeyRef)
	in.DeepCopyInto(out)
	return out
}

func (in *SecretKeyRef) SetDefaults(r SecretKeyRef) {
	if in.Secret == "" {
		in.Secret = r.Secret
	}
	if in.Key == "" {
		in.Key = r.Key
	}
	if in.Optional == nil {
		in.Optional = r.Optional
	}
}
