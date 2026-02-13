#!/bin/bash

images=("pytorch-cuda" "pytorch-rocm" "pytorch-cpu")
path=$(dirname $(readlink -f $0))
for image in "${images[@]}"; do
	cd $path/$image
	docker build -t $image .
done

for image in "${images[@]}"; do
	cd $path/$image
	sh ./run_test.sh $image
done