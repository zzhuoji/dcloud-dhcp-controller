```shell
# 1. service mapping 模式下，部署 dhcp controller
# 无需挂载附加网络，通过监听 lb svc 来启动 dhcp server , 这时候因为不在同一网段, 所以外部需要启动一个 dhcp 中继器,来指向 lb svc
dhcrelay 192.168.10.189

# lb svc 参考 sriov-svc.yaml


# 2. nic 模式下, 部署 dhcp controller
# 这时候 dhcp controller 会监听挂载的附加网络的网卡, 保证与 vm 在同一网段,会劫持 dhcp 请求



# 子网的设置可以对应多个 provider 

```