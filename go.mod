module github.com/coderanger/migrations-operator

go 1.15

// replace github.com/coderanger/controller-utils => /Users/coderanger/src/gm/controller-utils

replace sigs.k8s.io/controller-runtime => github.com/coderanger/controller-runtime v0.2.0-beta.1.0.20201115004253-9bec1fefa8ca

require (
	github.com/coderanger/controller-utils v0.0.0-20201115005928-cf3babb815dd
	github.com/go-logr/logr v0.1.0
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/pkg/errors v0.9.1
	golang.org/x/sys v0.0.0-20220325203850-36772127a21f // indirect
	k8s.io/api v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v0.18.6
	sigs.k8s.io/controller-runtime v0.6.3
)
