wget -q --show-progress --https-only --timestamping https://github.com/containernetworking/plugins/releases/download/v1.0.0/cni-plugins-linux-amd64-v1.0.0.tgz

sudo mkdir -p \
  /etc/cni/net.d \
  /opt/cni/bin


sudo tar -xvf cni-plugins-linux-amd64-v1.0.0.tgz -C /opt/cni/bin/


#d060239@lima-docker:/sys/fs/cgroup/memory/kubepods$ ip a
#1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
#    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
#    inet 127.0.0.1/8 scope host lo
#       valid_lft forever preferred_lft forever
#    inet6 ::1/128 scope host
#       valid_lft forever preferred_lft forever
#2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP group default qlen 1000
#    link/ether 52:55:55:e1:f7:37 brd ff:ff:ff:ff:ff:ff
#    altname enp0s1
#    inet 192.168.5.15/24 brd 192.168.5.255 scope global dynamic eth0

# using as pod CIDR the range indicated on eth0

#sipcalc 192.168.5.15/24
#-[ipv4 : 192.168.5.15/24] - 0
#
#[CIDR]
#Host address		- 192.168.5.15
#Host address (decimal)	- 3232236815
#Host address (hex)	- C0A8050F
#Network address		- 192.168.5.0
#Network mask		- 255.255.255.0
#Network mask (bits)	- 24
#Network mask (hex)	- FFFFFF00
#Broadcast address	- 192.168.5.255
#Cisco wildcard		- 0.0.0.255
#Addresses in network	- 256
#Network range		- 192.168.5.0 - 192.168.5.255
#Usable range		- 192.168.5.1 - 192.168.5.254

POD_CIDR=192.168.5.0/24

cat <<EOF | sudo tee /etc/cni/net.d/10-bridge.conf
{
    "cniVersion": "0.4.0",
    "name": "bridge",
    "type": "bridge",
    "bridge": "cnio0",
    "isGateway": true,
    "ipMasq": true,
    "ipam": {
        "type": "host-local",
        "ranges": [
          [{"subnet": "${POD_CIDR}"}]
        ],
        "routes": [{"dst": "0.0.0.0/0"}]
    }
}
EOF

cat <<EOF | sudo tee /etc/cni/net.d/99-loopback.conf
{
    "cniVersion": "0.4.0",
    "name": "lo",
    "type": "loopback"
}
EOF
