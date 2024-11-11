package subnet

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type Operation string

const (
	ADD    Operation = "add"
	UPDATE Operation = "update"
	DELETE Operation = "delete"
)

type Event struct {
	ObjKey    types.NamespacedName
	Operation Operation
	Provider  string
}

func (e Event) KeyString() string {
	return e.ObjKey.Name
}

func NewEvent(obj metav1.Object, provider string, operation Operation) Event {
	return Event{
		Operation: operation,
		Provider:  provider,
		ObjKey: types.NamespacedName{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		},
	}
}
