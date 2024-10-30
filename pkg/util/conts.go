package util

const (
	DCloudDomain = "dcloud.tydic.io"

	networkPrefix = "network." + DCloudDomain

	LabelDCloudLeader = networkPrefix + "/leader" // active

	AnnoDCloudEnableDHCP = networkPrefix + "/enable-dhcp" // true
)
