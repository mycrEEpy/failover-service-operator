# Failover Service Operator

[![Go Report Card](https://goreportcard.com/badge/github.com/mycreepy/failover-service-operator)](https://goreportcard.com/report/github.com/mycreepy/failover-service-operator)
[![Known Vulnerabilities](https://snyk.io/test/github/mycrEEpy/failover-service-operator/badge.svg)](https://snyk.io/test/github/mycrEEpy/failover-service-operator)

Kubernetes operator for providing active-passive services on top of headless services for statefulsets.

## Description

tba

## Getting Started

1. Deploy the operator:

```sh
kubectl apply -f deploy/deploy.yml
```

2. Optionally deploy the samples:

```sh
kubectl apply -k config/samples/
```

or

3. Create your own FailoverServices:

```yaml
apiVersion: mycreepy.github.io/v1alpha1
kind: FailoverService
metadata:
  name: example-failover-service
  namespace: default
spec:
  service:
    name: example-headless-service
```

## Contributing

tba

### How it works

This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/) 
which provides a reconcile function responsible for synchronizing resources untile the desired state is reached on the cluster 

### Test It Out

1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions

If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

