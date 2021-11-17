package helper

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	v1 "shadow.com/v1/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func GetRedisPodNames(redisConfig *v1.Redis) []string {
	podNames := make([]string, redisConfig.Spec.Replicas)
	fmt.Printf("%+v", redisConfig)
	for i := 0; i < redisConfig.Spec.Replicas; i++ {
		podNames[i] = fmt.Sprintf("%s-%d", redisConfig.Name, i)
	}

	fmt.Println("PodNames: ", podNames)
	return podNames
}

//  判断 redis  pod 是否能获取
func IsExistPod(podName string, redis *v1.Redis, client client.Client) bool {
	err := client.Get(context.Background(), types.NamespacedName{
		Namespace: redis.Namespace,
		Name:      podName,
	},
		&corev1.Pod{},
	)

	if err != nil {
		return false
	}
	return true
}

func IsExistInFinalizers(podName string, redis *v1.Redis) bool {
	for _, fPodName := range redis.Finalizers {
		if podName == fPodName {
			return true

		}
	}
	return false
}

func CreateRedis(client client.Client, redisConfig *v1.Redis, podName string, schema *runtime.Scheme) (string, error) {
	if IsExistPod(podName, redisConfig, client) {
		return "", nil
	}

	newPod := &corev1.Pod{}
	newPod.Name = podName
	newPod.Namespace = redisConfig.Namespace
	newPod.Spec.Containers = []corev1.Container{
		{
			Name:            podName,
			Image:           "redis:5-alpine",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Ports: []corev1.ContainerPort{
				{
					ContainerPort: int32(redisConfig.Spec.Port),
				},
			},
		},
	}

	//  set  owner  reference
	err := controllerutil.SetControllerReference(redisConfig, newPod, schema)
	if err != nil {
		return "", err
	}

	err = client.Create(context.Background(), newPod)
	return podName, err
}
