package main

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const leaderStatusFile = "/tmp/leader_status"

func main() {
	// Get in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		// If in-cluster config fails, use KUBECONFIG
		kubeconfig, exists := os.LookupEnv("KUBECONFIG")
		if !exists {
			panic("Failed to get in-cluster config and KUBECONFIG not set")
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			panic(err.Error())
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	id, err := os.Hostname()
	if err != nil {
		panic(err.Error())
	}

	// Get lease and namespace from environment variables
	leaseName, exists := os.LookupEnv("LEASE_NAME")
	if !exists {
		panic("LEASE_NAME not set")
	}

	namespace, exists := os.LookupEnv("NAMESPACE")
	if !exists {
		panic("NAMESPACE not set")
	}

	// Lock required for leader election
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: namespace,
		},
		Client: clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	// Try and become the leader
	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				// we're now the leader
				updateStatus(id)
			},
			OnStoppedLeading: func() {
				// we are not the leader anymore
				removeStatusFile()
			},
			OnNewLeader: func(identity string) {
				// we observe a new leader
				if identity != id {
					removeStatusFile()
				}
			},
		},
	})
}

func updateStatus(status string) {
	f, err := os.Create(leaderStatusFile)
	if err != nil {
		panic(err.Error())
	}
	defer f.Close()

	_, err = f.WriteString(status)
	if err != nil {
		panic(err.Error())
	}
	f.Sync()
}

func removeStatusFile() {
	if err := os.Remove(leaderStatusFile); err != nil {
		fmt.Printf("failed to remove %s, error: %s", leaderStatusFile, err)
	}
}
