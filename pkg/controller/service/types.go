package service

import (
	"context"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"tydic.io/dcloud-dhcp-controller/pkg/controller/subnet"
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
	return e.ObjKey.String()
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

type podCache interface {
	GetSelfPod() *corev1.Pod
}

type subnetNotify interface {
	GetSubnetsByDHCPProvider(provider string) ([]*kubeovnv1.Subnet, error)
	DeleteNetworkProvider(ctx context.Context, subnetKey types.NamespacedName, subnet *kubeovnv1.Subnet, provider string) error
	EnQueue(event subnet.Event)
}
