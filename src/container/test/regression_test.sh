cd ..
docker build -t pytorch-gpu .

# Test cuda
echo "TEST CUDA"
cuda_is_available=$(docker run -it --rm --gpus all pytorch-gpu python3 -c "import torch; print(torch.cuda.is_available())")
echo $cuda_is_available
if [[ "$cuda_is_available" == "False" ]]; then
	echo "FAIL: CUDA is not available"
else
	echo "SUCCESS: CUDA is available"
fi

# Test sudo
echo "TEST SUDO"
test_sudo=$(docker run -it --rm --gpus all pytorch-gpu bash -c "sudo whoami" | grep -c "command not found")
echo $test_sudo
if [[ "$test_sudo" == "1" ]]; then
	echo "SUCCESS: sudo is disabled"
else
	echo "FAIL: sudo is enabled"
fi

# Test apt
echo "TEST APT"
test_apt=$(docker run -it --rm --gpus all pytorch-gpu bash -c "apt update" | grep -c "Permission denied")
echo $test_apt
if [[ "$test_apt" == "1" ]]; then
	echo "SUCCESS: apt is disabled"
else
	echo "FAIL: apt is enabled"
fi