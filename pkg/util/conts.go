package util

const (
	DCloudDomain = "dcloud.tydic.io"

	networkPrefix = "network." + DCloudDomain

	LabelDCloudLeader = networkPrefix + "/leader" // active

	AnnoDCloudDHCPProvider = networkPrefix + "/dhcp-provider"

	AnnoDCloudEnableDHCP = networkPrefix + "/enable-dhcp" // true
)
