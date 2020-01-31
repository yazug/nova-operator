package novacompute

import (
	"context"
        "strings"
        "regexp"
        //"fmt"

	novav1 "github.com/nova-operator/pkg/apis/nova/v1"
        appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_novacompute")
var ospHostAliases = []corev1.HostAlias{}

// TODO move to spec like image urls?
const (
        COMMON_CONFIGMAP_NAME   string = "common-config"
        NOVA_CONFIGMAP_NAME     string = "nova-config"
)

// Add creates a new NovaCompute Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileNovaCompute{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("novacompute-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource NovaCompute
	err = c.Watch(&source.Kind{Type: &novav1.NovaCompute{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner NovaCompute
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &novav1.NovaCompute{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileNovaCompute implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileNovaCompute{}

// ReconcileNovaCompute reconciles a NovaCompute object
type ReconcileNovaCompute struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a NovaCompute object and makes changes based on the state read
// and what is in the NovaCompute.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileNovaCompute) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling NovaCompute")

	// Fetch the NovaCompute instance
	instance := &novav1.NovaCompute{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

        commonConfigMap := &corev1.ConfigMap{}
        // TODO: to update hosts infocheck configmap ResourceVersion and update if needed.
        //currentConfigVersion := commonConfigMap.ResourceVersion

        reqLogger.Info("Creating host entries from config map:", "configMap: ", COMMON_CONFIGMAP_NAME)
        err = r.client.Get(context.TODO(), types.NamespacedName{Name: COMMON_CONFIGMAP_NAME, Namespace: instance.Namespace}, commonConfigMap)

        if err := controllerutil.SetControllerReference(instance, commonConfigMap, r.scheme); err != nil {
                return reconcile.Result{}, err
        }
        ospHostAliases = createOspHostsEntries(commonConfigMap)

        // Define a new Pod object
        pod := newDaemonset(instance)

	// Set NovaCompute instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Pod already exists
	found := &appsv1.DaemonSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		err = r.client.Create(context.TODO(), pod)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Pod created successfully - don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	// Pod already exists - don't requeue
	reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	return reconcile.Result{}, nil
}

func createOspHostsEntries(commonConfigMap *corev1.ConfigMap) []corev1.HostAlias{
        hostAliases := []corev1.HostAlias{}

        hostsFile := commonConfigMap.Data["hosts"]
        re := regexp.MustCompile(`(?s).*BEGIN ANSIBLE MANAGED BLOCK\n(.*)# END ANSIBLE MANAGED BLOCK.*`)

        hostsFile = re.FindStringSubmatch(hostsFile)[1]

        for _, hostRecord := range strings.Split(hostsFile, "\n") {
                if len(hostRecord) > 0 {
                        var ip string
                        var names []string

                        for i, r := range strings.Fields(hostRecord) {
                                if i == 0 {
                                        ip = r
                                } else {
                                        names = append(names, r)
                                }
                        }

                        hostAlias := corev1.HostAlias{
                                IP: ip,
                                Hostnames: names,
                        }
                        hostAliases = append(hostAliases, hostAlias)
                }
        }

        return hostAliases
}

func newDaemonset(cr *novav1.NovaCompute) *appsv1.DaemonSet {
        var bidirectional corev1.MountPropagationMode = corev1.MountPropagationBidirectional
        var hostToContainer corev1.MountPropagationMode = corev1.MountPropagationHostToContainer
        var trueVar bool = true
        var configVolumeDefaultMode int32 = 0644
        var dirOrCreate corev1.HostPathType = corev1.HostPathDirectoryOrCreate

        daemonSet := appsv1.DaemonSet{
                TypeMeta: metav1.TypeMeta{
                        Kind:       "DaemonSet",
                        APIVersion: "apps/v1",
                },
                ObjectMeta: metav1.ObjectMeta{
                        Name:      cr.Name + "-daemonset",
                        //Name:      fmt.Sprintf("%s-nova-%s",cr.Name, cr.Spec.NodeName),
                        Namespace: cr.Namespace,
                        //OwnerReferences: []metav1.OwnerReference{
                        //      *metav1.NewControllerRef(cr, schema.GroupVersionKind{
                        //              Group:   v1beta1.SchemeGroupVersion.Group,
                        //              Version: v1beta1.SchemeGroupVersion.Version,
                        //              Kind:    "GenericDaemon",
                        //      }),
                        //},
                },
                Spec: appsv1.DaemonSetSpec{
                        Selector: &metav1.LabelSelector{
                                // MatchLabels: map[string]string{"daemonset": cr.Spec.NodeName + cr.Name + "-daemonset"},
                                MatchLabels: map[string]string{"daemonset": cr.Name + "-daemonset"},
                        },
                        Template: corev1.PodTemplateSpec{
                                ObjectMeta: metav1.ObjectMeta{
                                        // Labels: map[string]string{"daemonset": cr.Spec.NodeName + cr.Name + "-daemonset"},
                                        Labels: map[string]string{"daemonset": cr.Name + "-daemonset"},
                                },
                                Spec: corev1.PodSpec{
                                        NodeSelector:   map[string]string{"daemon": cr.Spec.Label},
                                        HostNetwork:    true,
                                        HostPID:        true,
                                        DNSPolicy:      "ClusterFirstWithHostNet",
                                        HostAliases:    ospHostAliases,
                                        InitContainers: []corev1.Container{},
                                        Containers:     []corev1.Container{},
                                },
                        },
                },
        }

        // Add hosts entries rendered from the the config map to the hosts file of the containers in the pod
        // TODO:
        //       - move to some common lib to be used from nova and neutron operator
        //
        //for _, ospHostAlias := range ospHostAliases {
        //        daemonSet.Spec.Template.Spec.HostAliases = append(daemonSet.Spec.Template.Spec.HostAliases, ospHostAlias)
        //}

        initContainerSpec := corev1.Container{
                Name:  "nova-compute-config-init",
                Image: cr.Spec.NovaComputeImage,
                SecurityContext: &corev1.SecurityContext{
                        Privileged:  &trueVar,
                },
                Command: []string{
                        "/bin/bash", "-c", "export CTRL_IP_INTRENALAPI=$(getent hosts controller-0.internalapi | awk '{print $1}') && export POD_IP_INTERNALAPI=$(ip route get $CTRL_IP_INTRENALAPI | awk '{print $5}') && cp /etc/nova/nova.conf /mnt/nova.conf && crudini --set /mnt/nova.conf DEFAULT my_ip $POD_IP_INTERNALAPI && crudini --set /mnt/nova.conf vnc server_listen $POD_IP_INTERNALAPI && crudini --set /mnt/nova.conf vnc server_proxyclient_address $POD_IP_INTERNALAPI && echo $OSP_CTRL_HOST >> /mnt/ctrl_host",
                },
                Env: []corev1.EnvVar{
                        {
                                Name: "MY_POD_IP",
                                ValueFrom: &corev1.EnvVarSource{
                                        FieldRef: &corev1.ObjectFieldSelector{
                                                FieldPath: "status.podIP",
                                        },
                                },
                        },
//                        {
//                                Name: "OSP_CTRL_HOST",
//                                ValueFrom: &corev1.EnvVarSource{
//                                        ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
//                                                Name: "common-config",
//                                                Key: "osp_controller_hostname",
//                                        },
//                                },
//                        },
                },
                VolumeMounts: []corev1.VolumeMount{
                        {
                                Name:      "nova-config",
                                ReadOnly:  true,
                                MountPath: "/etc/nova/nova.conf",
                                SubPath:   "nova.conf",
                        },
                        {
                                Name:      "common-config",
                                ReadOnly:  true,
                                MountPath: "/common-config",
                        },
                        {
                                Name:      "etc-machine-id",
                                MountPath: "/etc/machine-id",
                                ReadOnly:  true,
                        },
                        {
                                Name:      "rendered-config-vol",
                                MountPath: "/mnt",
                                ReadOnly:  false,
                        },
                },
        }
        daemonSet.Spec.Template.Spec.InitContainers = append(daemonSet.Spec.Template.Spec.InitContainers, initContainerSpec)

        novaContainerSpec := corev1.Container{
                Name:  "nova-compute",
                Image: cr.Spec.NovaComputeImage,
                //ReadinessProbe: &corev1.Probe{
                //        Handler: corev1.Handler{
                //                Exec: &corev1.ExecAction{
                //                        Command: []string{
                //                                "/openstack/healthcheck",
                //                        },
                //                },
                //        },
                //        InitialDelaySeconds: 30,
                //        PeriodSeconds:       30,
                //        TimeoutSeconds:      1,
                //},
                Command: []string{
                        "/usr/bin/nova-compute", "--config-file", "/mnt/nova.conf",
                },
                SecurityContext: &corev1.SecurityContext{
                        Privileged:  &trueVar,
                },
                VolumeMounts: []corev1.VolumeMount{
                        {
                                Name:      "nova-config",
                                ReadOnly:  true,
                                MountPath: "/etc/nova/nova.conf",
                                SubPath:   "nova.conf",
                        },
                        {
                                Name:      "etc-libvirt-qemu-volume",
                                MountPath: "/etc/libvirt/qemu",
                                MountPropagation: &bidirectional,
                        },
                        {
                                Name:      "etc-machine-id",
                                MountPath: "/etc/machine-id",
                                ReadOnly:  true,
                        },
                        {
                                Name:      "boot-volume",
                                MountPath: "/boot",
                                MountPropagation: &hostToContainer,
                        },
                        {
                                Name:      "dev-volume",
                                MountPath: "/dev",
                                MountPropagation: &hostToContainer,
                        },
                        {
                                Name:      "lib-modules-volume",
                                MountPath: "/lib/modules",
                                MountPropagation: &hostToContainer,
                        },
                        {
                                Name:      "run-volume",
                                MountPath: "/run",
                                MountPropagation: &hostToContainer,
                        },
                        {
                                Name:      "sys-fs-cgroup-volume",
                                MountPath: "/sys/fs/cgroup",
                                ReadOnly:  true,
                        },
                        {
                                Name:      "run-libvirt-volume",
                                MountPath: "/var/run/libvirt",
                                MountPropagation: &bidirectional,
                        },
                        {
                                Name:      "nova-log-volume",
                                MountPath: "/var/log/nova",
                                MountPropagation: &bidirectional,
                        },
                        {
                                Name:      "var-lib-nova-volume",
                                MountPath: "/var/lib/nova",
                                MountPropagation: &bidirectional,
                        },
                        {
                                Name:      "var-lib-libvirt-volume",
                                MountPath: "/var/lib/libvirt",
                                MountPropagation: &bidirectional,
                        },
                        {
                                Name:      "rendered-config-vol",
                                MountPath: "/mnt",
                                ReadOnly:  true,
                        },
                },
        }
        daemonSet.Spec.Template.Spec.Containers = append(daemonSet.Spec.Template.Spec.Containers, novaContainerSpec)

        volConfigs := []corev1.Volume{
                {
                        Name: "boot-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/boot",
                                },
                        },
                },
                {
                        Name: "etc-libvirt-qemu-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/opt/osp/etc/libvirt/qemu",
                                        Type: &dirOrCreate,
                                },
                        },
                },
                {
                        Name: "run-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/run",
                                },
                        },
                },
                {
                        Name: "lib-modules-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/lib/modules",
                                },
                        },
                },
                {
                        Name: "dev-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/dev",
                                },
                        },
                },
                {
                        Name: "etc-machine-id",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/etc/machine-id",
                                },
                        },
                },
                {
                        Name: "sys-fs-cgroup-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/sys/fs/cgroup",
                                },
                        },
                },
                {
                        Name: "run-libvirt-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/var/run/libvirt",
                                        Type: &dirOrCreate,
                                },
                        },
                },
                {
                        Name: "var-lib-nova-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/var/lib/nova",
                                        Type: &dirOrCreate,
                                },
                        },
                },
                {
                        Name: "var-lib-libvirt-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/var/lib/libvirt",
                                        Type: &dirOrCreate,
                                },
                        },
                },
                {
                        Name: "nova-log-volume",
                        VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                        Path: "/var/log/containers/nova",
                                        Type: &dirOrCreate,
                                },
                        },
                },
                {
                        Name: "nova-config",
                        VolumeSource: corev1.VolumeSource{
                                ConfigMap: &corev1.ConfigMapVolumeSource{
                                         DefaultMode: &configVolumeDefaultMode,
                                         LocalObjectReference: corev1.LocalObjectReference{
                                                 Name: NOVA_CONFIGMAP_NAME,
                                         },
                                },
                        },
                },
                {
                        Name: "common-config",
                        VolumeSource: corev1.VolumeSource{
                                ConfigMap: &corev1.ConfigMapVolumeSource{
                                         DefaultMode: &configVolumeDefaultMode,
                                         LocalObjectReference: corev1.LocalObjectReference{
                                                 Name: COMMON_CONFIGMAP_NAME,
                                         },
                                },
                        },
                },
                {
                        Name: "rendered-config-vol",
                        VolumeSource: corev1.VolumeSource{
                                EmptyDir: &corev1.EmptyDirVolumeSource{},
                        },
                },
        }
        for _, volConfig := range volConfigs {
                daemonSet.Spec.Template.Spec.Volumes = append(daemonSet.Spec.Template.Spec.Volumes, volConfig)
        }

        return &daemonSet
}