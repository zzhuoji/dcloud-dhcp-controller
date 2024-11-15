package util

const (
	DCloudDomain  = "dcloud.tydic.io"
	networkPrefix = "network." + DCloudDomain

	// LabelDCloudLeader Indicate that a pod instance is the leader,
	// which can accurately hit the endpoint for the service.
	LabelDCloudLeader = networkPrefix + "/leader" // active
	// AnnoDCloudDHCPProvider Applied to Subnet annotations,
	// Indicate the DHCP network provider used by the Subnet.
	AnnoDCloudDHCPProvider = networkPrefix + "/dhcp-provider"
	// AnnoDCloudMappingProvider Applied to Service annotations,
	// Specify the mapping provider for LoadBalancer type Service.
	AnnoDCloudMappingProvider = networkPrefix + "/mapping-provider"

	//AnnoDCloudEnableDHCP = networkPrefix + "/enable-dhcp" // true
)
