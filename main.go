// Package main provides a Kubernetes sidecar that performs leader election.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

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

	id, exists := os.LookupEnv("MYID")
	if !exists {
		panic("aaaaa")
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

	// Create a context that we can cancel for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Track leadership status
	var isLeader bool
	var leaderMutex sync.Mutex

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start signal handler in a separate goroutine
	go func() {
		sig := <-sigChan
		fmt.Printf("Received signal %v, initiating graceful shutdown...\n", sig)

		// Check if we're the current leader before releasing the lease
		leaderMutex.Lock()
		if isLeader {
			fmt.Printf("Releasing lease as current leader...\n")
			// Release the lease by deleting it
			err := clientset.CoordinationV1().Leases(namespace).Delete(context.Background(), leaseName, metav1.DeleteOptions{})
			if err != nil {
				fmt.Printf("Failed to release lease: %v\n", err)
			} else {
				fmt.Printf("Lease released successfully\n")
			}
		}
		leaderMutex.Unlock()

		cancel() // Cancel the context to stop leader election
	}()

	// Try and become the leader
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: 5 * time.Second,
		RenewDeadline: 1 * time.Second,
		RetryPeriod:   100 * time.Millisecond,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				// we're now the leader
				leaderMutex.Lock()
				isLeader = true
				leaderMutex.Unlock()
				fmt.Printf("Started leading with identity: %s\n", id)
			},
			OnStoppedLeading: func() {
				// we are not the leader anymore
				leaderMutex.Lock()
				isLeader = false
				leaderMutex.Unlock()
				fmt.Printf("Stopped leading\n")
			},
			OnNewLeader: func(identity string) {
				// we observe a new leader
				if identity != id {
					leaderMutex.Lock()
					isLeader = false
					leaderMutex.Unlock()
					fmt.Printf("New leader elected: %s\n", identity)
				}
			},
		},
	})
}
