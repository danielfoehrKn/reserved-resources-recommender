#!/bin/bash
# Need to provide CRI FIFO to write to as first argument
while :
do
	date > $1
	sleep 0.1
done