#!/bin/bash
set -x

# use a recent debian image (e.g debian:latest) - GL kernel is typically up-to date so you want to match that. Also be able to get recent bcc package.
# then fetch the kernel headers for the underlying GL OS. The kernel headers must match the GL host's kernel version

# check if host runs Gardenlinx
if grep -q "GARDENLINUX" /host/etc/os-release
then
    echo "host is using gardenlinux"
else
    # code if not found
    echo "host is not running gardenlinux. This script is only for GL. For other OS's please install generic headers"
    exit 0
fi

# runnning GL, try to install via package manager, as Gardenlinux does not use
# a stock Kernel from  https://www.kernel.org/pub/linux/kernel/v${major_version}.x/linux-$kernel_version.tar.gz 
# - adds drivers and apply Debian Kernel patches from debian

# CAVEAT: container image where this script is run, must be able to install all dependent packages required by the GL kernel-headers package (check curl -s http://repo.gardenlinux.io/gardenlinux/dists/934.10/main/binary-amd64/Packages)
# - if the image is older, than e.g gcc-13 is not available yet, hence headers cannot be installed.

apt update
# required to obtain GL apt repository public key
apt -y install curl

# get GL version
GL_VERSION=$(cat /host/etc/os-release | grep "GARDENLINUX_VERSION_AT_BUILD" | cut -d "=" -f 2)

touch /etc/apt/sources.list

# add unstable because contains newer packages that might be required for the GL Kernel Headers
echo "deb http://deb.debian.org/debian/ unstable main contrib" >> /etc/apt/sources.list

# add the GL apt repository for the GL correct version (not kernel version, but GL apt-repository has the kernel headers for the kernel used in the GL version)
echo "deb [arch=amd64 signed-by=/etc/apt/trusted.gpg.d/gardenlinux.asc] https://repo.gardenlinux.io/gardenlinux $GL_VERSION main" >> /etc/apt/sources.list

# requires the public key for the apt-repository
curl https://raw.githubusercontent.com/gardenlinux/gardenlinux/main/gardenlinux.asc -o /etc/apt/trusted.gpg.d/gardenlinux.asc

#install required packages
apt update

# install bcc
apt-get install -y bpfcc-tools libbpfcc libbpfcc-dev

# install kernel headers for GL
KERNEL_VERSION="${KERNEL_VERSION:-$(uname -r)}"
kernel_version="$(echo "${KERNEL_VERSION}" | awk -vFS='[-+]' '{ print $1 }')"
echo "Fetching upstream kernel sources for ${kernel_version}."

# should show the kernel headers for the container image (because sources.list contains the container images default package repository) as well as for the host's GL kernel
# - linux-headers-5.15.125-gardenlinux-cloud-amd64/934.10
apt search linux-headers- | grep -i "gardenlinux"

# only works for amd64 architecture at the moment
echo "installing Kernel headers for GL $GL_VERSION: linux-headers-$kernel_version-gardenlinux-cloud-amd64"

apt install -y linux-headers-$kernel_version-gardenlinux-cloud-amd64



