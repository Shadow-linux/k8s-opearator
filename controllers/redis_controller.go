/*
Copyright 2021 shadow.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"shadow.com/v1/helper"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	myappv1 "shadow.com/v1/api/v1"
)

// RedisReconciler reconciles a Redis object
type RedisReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	EventRecord record.EventRecorder
}

//+kubebuilder:rbac:groups=myapp.shadow.com,resources=redis,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=myapp.shadow.com,resources=redis/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=myapp.shadow.com,resources=redis/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Redis object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RedisReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	redis := &myappv1.Redis{}
	if err := r.Get(ctx, req.NamespacedName, redis); err != nil {
		fmt.Println(err)
	} else {
		//  如果不为空 则正在删除
		if !redis.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.clearRedis(ctx, redis)
		}

		fmt.Printf("得到对象 %+v \n", redis.Spec)
		podNames := helper.GetRedisPodNames(redis)
		isEdit := false
		for _, podName := range podNames {
			podName, err := helper.CreateRedis(r.Client, redis, podName, r.Scheme)

			if err != nil {
				return ctrl.Result{}, err
			}

			if podName == "" {
				continue
			}

			if controllerutil.ContainsFinalizer(redis, podName) {
				continue
			}

			redis.Finalizers = append(redis.Finalizers, podName)
			isEdit = true
		}

		//  副本收缩
		if len(redis.Finalizers) > len(podNames) {
			r.EventRecord.Event(redis, corev1.EventTypeNormal, "Upgrade", "副本收缩")
			isEdit = true
			err := r.rmIfSurplus(ctx, podNames, redis)
			if err != nil {
				return ctrl.Result{}, err
			}
		}

		if isEdit {
			r.EventRecord.Event(redis, corev1.EventTypeNormal, "Updated", "更新 shadow-redis")

			err = r.Client.Update(ctx, redis)
			if err != nil {
				return ctrl.Result{}, err
			}
			redis.Status.RedisNum = len(redis.Finalizers)
			err = r.Status().Update(ctx, redis)
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil

	}

	return ctrl.Result{}, nil
}

// 收缩副本 ['redis0','redis1']   ---> podName ['redis0']
func (r *RedisReconciler) rmIfSurplus(ctx context.Context, podNames []string, redis *myappv1.Redis) error {
	for i := 0; i < len(redis.Finalizers)-len(podNames); i++ {
		err := r.Client.Delete(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: redis.Finalizers[len(podNames)+i], Namespace: redis.Namespace,
			},
		})

		if err != nil {
			return err
		}
	}
	redis.Finalizers = podNames
	return nil
}

func (r *RedisReconciler) clearRedis(ctx context.Context, redis *myappv1.Redis) error {
	podList := redis.Finalizers
	for _, podName := range podList {
		err := r.Client.Delete(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: redis.Namespace,
			},
		})

		if err != nil {
			fmt.Println("清除Pod异常：", err)
		}
	}

	redis.Finalizers = []string{}
	return r.Client.Update(ctx, redis)
}

func (r *RedisReconciler) podDeleteHandler(event event.DeleteEvent, limitInterface workqueue.RateLimitingInterface) {
	fmt.Println("被删除的对象名称是", event.Object.GetName())

	for _, ref := range event.Object.GetOwnerReferences() {
		// 因为会获取到所有被删除的pod，所以进行一次判断
		if ref.Kind == "Redis" && ref.APIVersion == "myapp.shadow.com/v1" {
			// 重新推送队列，进行 reconcile
			limitInterface.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      ref.Name,
					Namespace: event.Object.GetNamespace(),
				},
			})
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *RedisReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&myappv1.Redis{}).
		// 监控资源，并对delete动作进行操作
		Watches(
			&source.Kind{Type: &corev1.Pod{}},
			handler.Funcs{DeleteFunc: r.podDeleteHandler},).
		Complete(r)
}
