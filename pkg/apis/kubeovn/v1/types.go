package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

type Subnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubnetSpec   `json:"spec"`
	Status SubnetStatus `json:"status,omitempty"`
}

type SubnetSpec struct {
	Default    bool     `json:"default"`
	Vpc        string   `json:"vpc,omitempty"`
	Protocol   string   `json:"protocol,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	CIDRBlock  string   `json:"cidrBlock"`
	Gateway    string   `json:"gateway"`
	ExcludeIps []string `json:"excludeIps,omitempty"`
	Provider   string   `json:"provider,omitempty"`

	GatewayType string `json:"gatewayType,omitempty"`
	GatewayNode string `json:"gatewayNode"`
	NatOutgoing bool   `json:"natOutgoing"`

	ExternalEgressGateway string `json:"externalEgressGateway,omitempty"`
	PolicyRoutingPriority uint32 `json:"policyRoutingPriority,omitempty"`
	PolicyRoutingTableID  uint32 `json:"policyRoutingTableID,omitempty"`
	Mtu                   uint32 `json:"mtu,omitempty"`

	Private      bool     `json:"private"`
	AllowSubnets []string `json:"allowSubnets,omitempty"`

	Vlan string   `json:"vlan,omitempty"`
	Vips []string `json:"vips,omitempty"`

	LogicalGateway         bool `json:"logicalGateway,omitempty"`
	DisableGatewayCheck    bool `json:"disableGatewayCheck,omitempty"`
	DisableInterConnection bool `json:"disableInterConnection,omitempty"`

	EnableDHCP    bool   `json:"enableDHCP,omitempty"`
	DHCPv4Options string `json:"dhcpV4Options,omitempty"`
	DHCPv6Options string `json:"dhcpV6Options,omitempty"`

	EnableIPv6RA  bool   `json:"enableIPv6RA,omitempty"`
	IPv6RAConfigs string `json:"ipv6RAConfigs,omitempty"`

	Acls           []ACL `json:"acls,omitempty"`
	AllowEWTraffic bool  `json:"allowEWTraffic,omitempty"`

	NatOutgoingPolicyRules []NatOutgoingPolicyRule `json:"natOutgoingPolicyRules,omitempty"`

	U2OInterconnectionIP string `json:"u2oInterconnectionIP,omitempty"`
	U2OInterconnection   bool   `json:"u2oInterconnection,omitempty"`
	EnableLb             *bool  `json:"enableLb,omitempty"`
	EnableEcmp           bool   `json:"enableEcmp,omitempty"`
	EnableMulticastSnoop bool   `json:"enableMulticastSnoop,omitempty"`

	RouteTable         string                 `json:"routeTable,omitempty"`
	NamespaceSelectors []metav1.LabelSelector `json:"namespaceSelectors,omitempty"`
}

type ACL struct {
	Direction string `json:"direction,omitempty"`
	Priority  int    `json:"priority,omitempty"`
	Match     string `json:"match,omitempty"`
	Action    string `json:"action,omitempty"`
}

type NatOutgoingPolicyRule struct {
	Match  NatOutGoingPolicyMatch `json:"match"`
	Action string                 `json:"action"`
}

type NatOutgoingPolicyRuleStatus struct {
	RuleID string `json:"ruleID"`
	NatOutgoingPolicyRule
}

type NatOutGoingPolicyMatch struct {
	SrcIPs string `json:"srcIPs,omitempty"`
	DstIPs string `json:"dstIPs,omitempty"`
}

// ConditionType encodes information on the condition
type ConditionType string

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type Condition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// SubnetCondition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type SubnetCondition Condition

type SubnetStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []SubnetCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	V4AvailableIPs         float64                       `json:"v4availableIPs"`
	V4AvailableIPRange     string                        `json:"v4availableIPrange"`
	V4UsingIPs             float64                       `json:"v4usingIPs"`
	V4UsingIPRange         string                        `json:"v4usingIPrange"`
	V6AvailableIPs         float64                       `json:"v6availableIPs"`
	V6AvailableIPRange     string                        `json:"v6availableIPrange"`
	V6UsingIPs             float64                       `json:"v6usingIPs"`
	V6UsingIPRange         string                        `json:"v6usingIPrange"`
	ActivateGateway        string                        `json:"activateGateway"`
	DHCPv4OptionsUUID      string                        `json:"dhcpV4OptionsUUID"`
	DHCPv6OptionsUUID      string                        `json:"dhcpV6OptionsUUID"`
	U2OInterconnectionIP   string                        `json:"u2oInterconnectionIP"`
	U2OInterconnectionMAC  string                        `json:"u2oInterconnectionMAC"`
	U2OInterconnectionVPC  string                        `json:"u2oInterconnectionVPC"`
	NatOutgoingPolicyRules []NatOutgoingPolicyRuleStatus `json:"natOutgoingPolicyRules"`
	McastQuerierIP         string                        `json:"mcastQuerierIP"`
	McastQuerierMAC        string                        `json:"mcastQuerierMAC"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SubnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Subnet `json:"items"`
}
