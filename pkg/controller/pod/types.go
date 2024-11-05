package pod

import (
	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
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
}

func NewEvent(obj metav1.Object, operation Operation) Event {
	return Event{
		Operation: operation,
		ObjKey: types.NamespacedName{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		},
	}
}

type PendingNetwork struct {
	SubnetName      string
	MultusName      string
	MultusNamespace string
	networkv1.NetworkStatus
}
