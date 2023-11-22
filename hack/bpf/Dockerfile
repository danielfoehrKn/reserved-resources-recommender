FROM debian:bookworm


RUN apt update && apt install -y bpfcc-tools libbpfcc libbpfcc-dev
RUN apt install -y procps # to have ps installed
RUN ls /usr/sbin/ | grep bpfcc

# add package repository for FL 934.7, required to get kernel header package for bcc
RUN echo "deb [arch=amd64 signed-by=/etc/apt/trusted.gpg.d/gardenlinux.asc] https://repo.gardenlinux.io/gardenlinux 934.10 main" >> /etc/apt/sources.list

# get GL repository GPG key (otherwise apt install fails)
RUN wget https://raw.githubusercontent.com/gardenlinux/gardenlinux/main/gardenlinux.asc
RUN cat gardenlinux.asc > /etc/apt/trusted.gpg.d/gardenlinux.asc

# then install kernel headers for correct version
RUN apt update
RUN apt install -y linux-headers-5.15.103-gardenlinux-cloud-amd64

CMD ["/usr/bin/bash"]