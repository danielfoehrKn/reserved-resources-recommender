# remount gardenlinux
PARTITION=$(mount -v | grep "^/.*/usr" | awk '{print $1}')
mount -o remount,rw ${PARTITION} /usr

version=0.18.0
# https://github.com/containerd/nerdctl/releases/download/v0.18.0/nerdctl-0.18.0-linux-amd64.tar.gz
wget https://github.com/containerd/nerdctl/releases/download/v$version/nerdctl-$version-linux-amd64.tar.gz
sudo tar -xvf nerdctl-$version-linux-amd64.tar.gz --directory /usr/local/bin
chmod +x /usr/local/bin/nerdctl
alias nerdctl='nerdctl -n k8s.io'