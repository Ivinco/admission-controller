package admissioncontroller

import (
	"fmt"

	v1 "k8s.io/api/admission/v1"
)

// Result contains the result of an admission request
type Result struct {
	Allowed  bool
	Msg      string
	PatchOps []PatchOperation
}

// AdmitFunc defines how to process an admission request
type AdmitFunc func(request *v1.AdmissionRequest) (*Result, error)

// Hook represents the set of functions for each operation in an admission webhook.
type Hook struct {
	Create  AdmitFunc
	Delete  AdmitFunc
	Update  AdmitFunc
	Connect AdmitFunc
}

// Execute evaluates the request and try to execute the function for operation specified in the request.
func (h *Hook) Execute(r *v1.AdmissionRequest) (*Result, error) {
	switch r.Operation {
	case v1.Create:
		return wrapperExecution(h.Create, r)
	case v1.Update:
		return wrapperExecution(h.Update, r)
	case v1.Delete:
		return wrapperExecution(h.Delete, r)
	case v1.Connect:
		return wrapperExecution(h.Connect, r)
	}

	return &Result{Msg: fmt.Sprintf("Invalid operation: %s", r.Operation)}, nil
}

func wrapperExecution(fn AdmitFunc, r *v1.AdmissionRequest) (*Result, error) {
	if fn == nil {
		return nil, fmt.Errorf("operation %s is not registered", r.Operation)
	}

	return fn(r)
}
