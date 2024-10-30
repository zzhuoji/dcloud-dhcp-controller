package util

import (
	"fmt"
	"os"
	"time"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/wait"
)

const NetworkStatusFilePath = "/etc/net-info/networks-status-map"

func FileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func readFileUntilNotEmpty(networkStatusMapPath string) ([]byte, error) {
	var networkStatusMapBytes []byte
	err := wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		var err error
		networkStatusMapBytes, err = os.ReadFile(networkStatusMapPath)
		return len(networkStatusMapBytes) > 0, err
	})
	return networkStatusMapBytes, err
}

func NetworkStatusFromFile(filePath string) ([]networkv1.NetworkStatus, error) {
	networkStatusMapBytes, err := readFileUntilNotEmpty(filePath)
	if err != nil {
		return nil, err
	}
	var networkStatusMap []networkv1.NetworkStatus
	err = json.Unmarshal(networkStatusMapBytes, &networkStatusMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal network-status map %w", err)
	}
	return networkStatusMap, nil
}
