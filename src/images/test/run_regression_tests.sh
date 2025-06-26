#!/bin/bash

function test_match() {
	local gpu_option=""

	while getopts ":g" opt; do
		case "$opt" in
		g)
			gpu_option="--gpus all"
			shift $((OPTIND - 1))
			;;
		\?)
			echo "Unknown option: -$OPTARG"
			return 1
			;;
		esac
	done
	local test_name=$1
	local command=$2
	local input=$3
	local expected=$4

	local actual=$(docker run -it --rm $gpu_option $image "$command" -c "$input" | grep -c "$expected")
	echo "Running test for $test_name"
	if [[ "$actual" == "1" ]]; then
		echo "SUCCESS"
	else
		echo "FAIL"
	fi
}

function test_no_match() {
	local gpu_option=""

	while getopts ":g" opt; do
		case "$opt" in
		g)
			gpu_option="--gpus all"
			shift $((OPTIND - 1))
			;;
		\?)
			echo "Unknown option: -$OPTARG"
			return 1
			;;
		esac
	done
	local test_name=$1
	local command=$2
	local input=$3
	local expected=$4

	local actual=$(docker run -it --rm $gpu_option $image "$command" -c "$input" | grep -c "$expected")
	echo "Running test for $test_name"
	if [[ "$actual" == "0" ]]; then
		echo "SUCCESS"
	else
		echo "FAIL"
	fi
}


cd ../pytorch-cuda
docker build -t pytorch-cuda .
image=pytorch-cuda

test_match -g "TEST CUDA" "python3" "import torch; print(torch.cuda.is_available())" "True"
test_match "TEST SUDO" "bash" "sudo whoami" "command not found"
test_match "TEST APT" "bash" "apt install" "Permission denied"

cd ../pytorch-cpu
docker build -t pytorch-cpu .
image=pytorch-cpu

test_match "TEST CUDA" "python3" "import torch; print(torch.cuda.is_available())" "False"
test_no_match "TEST SUDO" "cat" "/etc/sudoers" "guest"
test_match "TEST APT" "bash" "apt install" "Permission denied"

cd ../pytorch-rocm
docker build -t pytorch-rocm .
image=pytorch-rocm

test_match "TEST CUDA" "python3" "import torch; print(torch.cuda.is_available())" "False"
test_no_match "TEST SUDO" "cat" "/etc/sudoers" "guest"
test_match "TEST APT" "bash" "apt install" "Permission denied"
