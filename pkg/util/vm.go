package util

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

func GetVMKeyByPodKey(podKey types.NamespacedName) string {
	name := strings.TrimPrefix(podKey.Name, "virt-launcher-")
	if lastIndex := strings.LastIndex(name, "-"); lastIndex > 0 {
		name = name[:lastIndex]
	}
	return fmt.Sprintf("%s/%s", podKey.Namespace, name)
}
