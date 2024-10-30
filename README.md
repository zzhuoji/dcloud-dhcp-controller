# dcloud-dhcp-controller

dcloud-dhcp-controller 是为dcloud2.0 iaas平台服务的组件，为kubevirt虚拟机提供静态DHCP，其灵感来源于[kubevirt-ip-helper](https://github.com/joeyloman/kubevirt-ip-helper)

## Prerequisites

* Kubernetes
* kubevirt
* kube-ovn: dcloud-dhcp-controller依赖kube-ovn的Subnet资源划分网络范围，并使用它的IPAM插件设置容器网络
* Multus with bridge networking configured


## Building the container

There is a Dockerfile in the current directory which can be used to build the container, for example:

```SH
make docker-build
```

Then push it to the remote container registry target, for example:

```SH
make docker-push
```

## Deploying the container

Use the deployment.yaml template which is located in the templates directory, for example:

```SH
kubectl create -f deploy/deployment.yaml
```

Before executing the above command, edit the deployment.yaml and:

Configure the Multus NetworkAttachmentDefinition name and namespace:
```YAML
spec:
  [..]
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: '[{ "interface":"eth1","name":"<NETWORKATTACHMENTDEFINITION_NAME>","namespace":"<NAMESPACE>" }]'
```

> **_NOTE:_** Make sure to replace the \<NETWORKATTACHMENTDEFINITION_NAME> and \<NAMESPACE> placeholders.

### Logging

By default only the startup, error and warning logs are enabled. More logging can be enabled by changing the LOGLEVEL environment setting in the deployment. The supported loglevels are INFO, DEBUG and TRACE.

