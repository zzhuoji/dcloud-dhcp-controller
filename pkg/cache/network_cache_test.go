package cache

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	networkv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	ovnutil "github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/stretchr/testify/assert"
)

func Test_NetworkCache(t *testing.T) {
	OriginalNetworkInfos := []networkv1.NetworkStatus{
		{
			Name:      "ovn",
			Default:   true,
			Interface: "eth0",
			IPs: []string{
				"10.10.1.39",
			},
			Mtu: 1500,
			Mac: ovnutil.GenerateMac(),
		},
		{
			Name:      "default/net-atta-def",
			Default:   true,
			Interface: "net1",
			IPs: []string{
				"192.168.2.10",
			},
			Mtu: 1500,
			Mac: ovnutil.GenerateMac(),
		},
	}
	cache := NewNetworkCache(OriginalNetworkInfos)
	t.Run("Get default network", func(t *testing.T) {
		defaultNetwork, ok := cache.GetDefaultNetwork()
		if !ok {
			t.Errorf("Cannot find default network")
		}
		snap := snapshotNetworkStatus(OriginalNetworkInfos[0])
		assert.Equal(t, snap, *defaultNetwork)
	})

	t.Run("Get network status", func(t *testing.T) {
		name := OriginalNetworkInfos[1].Name
		networkStatus, ok := cache.GetNetworkStatus(name)
		if !ok {
			t.Errorf("Cannot find network %s", name)
		}
		snap := snapshotNetworkStatus(OriginalNetworkInfos[1])
		assert.Equal(t, snap, *networkStatus)
	})

	t.Run("Set network status", func(t *testing.T) {
		snap := snapshotNetworkStatus(OriginalNetworkInfos[0])
		t.Run("check network name", func(t *testing.T) {
			snap.Name = ""
			err := cache.SetNetworkStatus(snap)
			assert.Equal(t, fmt.Errorf("network name is empty"), err)
		})
		t.Run("check network default", func(t *testing.T) {
			snap.Name = OriginalNetworkInfos[0].Name
			err := cache.SetNetworkStatus(snap)
			assert.Equal(t, fmt.Errorf("cannot set default network"), err)
		})
		t.Run("check original network name", func(t *testing.T) {
			snap.Default = false
			err := cache.SetNetworkStatus(snap)
			assert.Equal(t, fmt.Errorf("cannot set the original network <%s>", snap.Name), err)
		})
		t.Run("check set network", func(t *testing.T) {
			snap.Name = "default/macvtap01"
			err := cache.SetNetworkStatus(snap)
			if err != nil {
				assert.Error(t, err)
			}
		})
		t.Run("check network name repeat", func(t *testing.T) {
			err := cache.SetNetworkStatus(snap)
			assert.Equal(t, fmt.Errorf("network name <%s> already exists", snap.Name), err)
		})
		t.Run("check get network", func(t *testing.T) {
			networkStatus, ok := cache.GetNetworkStatus(snap.Name)
			if !ok {
				t.Errorf("set network failed")
			}
			assert.Equal(t, snap, *networkStatus)
		})
	})
	t.Run("Update network status", func(t *testing.T) {
		var networkStatus *networkv1.NetworkStatus
		t.Run("check get network", func(t *testing.T) {
			var ok bool
			networkStatus, ok = cache.GetNetworkStatus("default/macvtap01")
			if !ok {
				t.Errorf("get network failed")
			}
		})
		t.Run("check network name", func(t *testing.T) {
			networkStatus := *networkStatus
			networkStatus.Name = ""
			err := cache.UpdateNetworkStatus(networkStatus)
			assert.Equal(t, fmt.Errorf("network name is empty"), err)
		})
		t.Run("check network default", func(t *testing.T) {
			networkStatus := *networkStatus
			networkStatus.Default = true
			err := cache.UpdateNetworkStatus(networkStatus)
			assert.Equal(t, fmt.Errorf("cannot set default network"), err)
		})
		t.Run("check original network name", func(t *testing.T) {
			networkStatus := *networkStatus
			networkStatus.Name = OriginalNetworkInfos[0].Name
			err := cache.UpdateNetworkStatus(networkStatus)
			assert.Equal(t, fmt.Errorf("cannot update the original network <%s>", networkStatus.Name), err)
		})
		t.Run("check network name non-existent", func(t *testing.T) {
			networkStatus := *networkStatus
			networkStatus.Name = "test"
			err := cache.UpdateNetworkStatus(networkStatus)
			assert.Equal(t, fmt.Errorf("network name <%s> non-existent", "test"), err)
		})
		t.Run("check update network", func(t *testing.T) {
			networkStatus := *networkStatus
			networkStatus.Mtu = 1400
			networkStatus.Mac = ovnutil.GenerateMac()
			err := cache.UpdateNetworkStatus(networkStatus)
			if err != nil {
				assert.Error(t, err)
			}
			status, ok := cache.GetNetworkStatus(networkStatus.Name)
			if !ok {
				t.Errorf("update network failed")
			}
			assert.Equal(t, networkStatus, *status)
		})
	})
	t.Run("Delete network status", func(t *testing.T) {
		t.Run("check delete empty network", func(t *testing.T) {
			err := cache.DeleteNetworkStatus("")
			assert.Equal(t, fmt.Errorf("network name is empty"), err)
		})
		t.Run("check delete original network", func(t *testing.T) {
			name := OriginalNetworkInfos[0].Name
			err := cache.DeleteNetworkStatus(name)
			assert.Equal(t, fmt.Errorf("cannot delete original network <%s>", name), err)
		})
		t.Run("check delete non-existent network", func(t *testing.T) {
			name := "test"
			err := cache.DeleteNetworkStatus(name)
			assert.Equal(t, fmt.Errorf("network name <%s> non-existent", name), err)
		})
		t.Run("check delete network", func(t *testing.T) {
			name := "default/macvtap01"
			err := cache.DeleteNetworkStatus(name)
			if err != nil {
				assert.Error(t, err)
			}
			status, ok := cache.GetNetworkStatus(name)
			if ok || status != nil {
				assert.Error(t, fmt.Errorf("delete network failed"))
			}
		})
	})
	t.Run("Reentrant lock", func(t *testing.T) {
		prefix := "default/macvlan"
		wait := sync.WaitGroup{}
		go func() {
			cache.Lock()
			network, _ := cache.GetDefaultNetwork()
			network.Default = false
			for i := 0; i < 10; i++ {
				network.Mac = ovnutil.GenerateMac()
				name := prefix + strconv.Itoa(i)
				network.Name = name
				err := cache.SetNetworkStatus(*network)
				if err != nil {
					assert.Error(t, err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			cache.Unlock()
			wait.Done()
		}()
		wait.Add(1)
		go func() {
			for i := 0; i < 10; i++ {
				name := prefix + strconv.Itoa(i)
				status, ok := cache.GetNetworkStatus(name)
				if !ok || status == nil {
					assert.Error(t, fmt.Errorf("get network failed"))
				}
			}
			wait.Done()
		}()
		wait.Add(1)
		wait.Wait()
	})
}
