# README

### Instructions to build and interact with container
```
docker build -t <image name> .
docker run -it --rm --gpus all <image name> bash
```
### Test
```
sh run_regression_tests.sh
```
