# nova-compute-operator
POC nova-compute-operator

NOTE: 
- The current functionality is on install at the moment, no update/upgrades or other features.
- At the moment only covers nova-compute related services (virtlogd/libvirtd/nova-compute)

## Pre Req:
- OSP16 with OVS instead of OVN deployed
- worker nodes have connection to internalapi and tenant network VLAN


#### Clone it

    $ git clone https://github.com/stuggi/nova-operator.git
    $ cd nova-operator

#### Updates required to pkg/controller/compute/compute_controller.go atm:
  - update opsHostAliases to reflect the hosts entries of your OSP env
  - update `ip route get 172.17.1.29` to reflect the overcloud.internalapi.localdomain IP address

#### Create the operator

Build the image
    
    $ oc create -f deploy/crds/nova_v1alpha1_compute_crd.yaml
    $ operator-sdk build <image e.g quay.io/mschuppe/nova-operator:v0.0.1>

Replace `image:` in deploy/operator.yaml with your image

    $ sed -i 's|REPLACE_IMAGE|quay.io/mschuppe/nova-operator:v0.0.1|g' deploy/operator.yaml
    $ podman push --authfile ~/mschuppe-auth.json quay.io/mschuppe/nova-operator:v0.0.1

Create role, binding service_account

    $ oc create -f deploy/role.yaml
    $ oc create -f deploy/role_binding.yaml
    $ oc create -f deploy/service_account.yaml

Create operator

    $ oc create -f deploy/operator.yaml

    POD=`oc get pods -l name=nova-operator --field-selector=status.phase=Running -o name | head -1 -`; echo $POD
    oc logs $POD -f

Create custom resource for a compute node which specifies the container images and the label
get latest container images from rdo rhel8-train from https://trunk.rdoproject.org/rhel8-train/current-tripleo/commit.yaml
or

    $ dnf install python2 python2-yaml
    $ python -c 'import urllib2;import yaml;c=yaml.load(urllib2.urlopen("https://trunk.rdoproject.org/rhel8-train/current-tripleo/commit.yaml"))["commits"][0];print "%s_%s" % (c["commit_hash"],c["distro_hash"][0:8])'
    f8b48998e5d600f24513848b600e84176ce90223_243bc231

Update `deploy/crds/nova_v1alpha1_compute_cr.yaml`

    apiVersion: nova.openstack-k8s-operators/v1alpha1
    kind: Compute
    metadata:
      name: nova-compute
    spec:
      novaLibvirtImage: trunk.registry.rdoproject.org/tripleotrain/rhel-binary-nova-libvirt:f8b48998e5d600f24513848b600e84176ce90223_243bc231
      novaComputeImage: trunk.registry.rdoproject.org/tripleotrain/rhel-binary-nova-compute:f8b48998e5d600f24513848b600e84176ce90223_243bc231
      label: compute

Apply `deploy/crds/nova_v1alpha1_compute_cr.yaml`

    $ oc apply -f deploy/crds/nova_v1alpha1_compute_cr.yaml

    $ oc get pods
    NAME                           READY   STATUS    RESTARTS   AGE
    nova-operator-ffd64796-vshg6   1/1     Running   0          119s

    $ oc get ds
    NAME                  DESIRED   CURRENT   READY   UP-TO-DATE   AVAILABLE   NODE SELECTOR    AGE
    nova-node-daemonset   0         0         0       0            0           daemon=compute   118s

    $ oc describe Compute 
    Name:         nova-compute
    Namespace:    default
    Labels:       <none>
    Annotations:  kubectl.kubernetes.io/last-applied-configuration:
                    {"apiVersion":"nova.openstack-k8s-operators/v1alpha1","kind":"Compute","metadata":{"annotations":{},"name":"nova-compute","namespace":"def...
    API Version:  nova.openstack-k8s-operators/v1alpha1
    Kind:         Compute
    Metadata:
      Creation Timestamp:  2020-01-24T14:07:04Z
      Generation:          1
      Resource Version:    3821131
      Self Link:           /apis/nova.openstack-k8s-operators/v1alpha1/namespaces/default/computes/nova-compute
      UID:                 cc462eef-3eb2-11ea-a590-5254002c0120
    Spec:
      Label:               compute
      Nova Compute Image:  trunk.registry.rdoproject.org/tripleotrain/rhel-binary-nova-compute:f8b48998e5d600f24513848b600e84176ce90223_243bc231
      Nova Libvirt Image:  trunk.registry.rdoproject.org/tripleotrain/rhel-binary-nova-libvirt:f8b48998e5d600f24513848b600e84176ce90223_243bc231
    Events:                <none>

### Create required configMaps
TODO: move passwords, connection urls, ... to Secret

Get the following configs from a compute node in the OSP env:
- /var/lib/config-data/puppet-generated/neutron/etc/neutron/neutron.conf
- /var/lib/config-data/puppet-generated/neutron/etc/neutron/plugins/ml2/openvswitch_agent.ini
- /var/lib/config-data/puppet-generated/nova_libvirt/etc/nova/nova.conf
- /var/lib/config-data/puppet-generated/nova_libvirt/etc/libvirt/libvirtd.conf
- /var/lib/config-data/puppet-generated/nova_libvirt/etc/libvirt/qemu.conf

Place each group in a config dir like:
- libvirt-conf
- nova-conf
- neutron-conf

Create the configMaps

    $ oc create configmap libvirt-config --from-file=./libvirt-conf/
    $ oc create configmap nova-config --from-file=./nova-conf/
    $ oc create configmap neutron-config --from-file=./neutron-conf/

Note: if a later update is needed do e.g.

    $ oc create configmap neutron-config --from-file=./neutron-conf/ --dry-run -o yaml | oc apply -f -

!! Make sure we have the OSP needed network configs on the worker nodes. The workers need to be able to reach the internalapi and tenant network !!

    $ oc get nodes
    NAME       STATUS   ROLES    AGE   VERSION
    master-0   Ready    master   8d    v1.14.6+8e46c0036
    worker-0   Ready    worker   8d    v1.14.6+8e46c0036
    worker-1   Ready    worker   8d    v1.14.6+8e46c0036

Label a worker node as compute

    # oc label nodes worker-0 daemon=compute --overwrite
    node/worker-0 labeled

    oc get daemonset
    NAME                     DESIRED   CURRENT   READY   UP-TO-DATE   AVAILABLE   NODE SELECTOR    AGE
    nova-compute-daemonset   1         1         1       1            1           daemon=compute   5m27s

    oc get pods
    NAME                             READY   STATUS    RESTARTS   AGE
    nova-compute-daemonset-gjjnm     3/3     Running   0          22s
    nova-operator-5d56d8459b-mbqrn   1/1     Running   0          7m58s

    oc get pods nova-compute-daemonset-gjjnm-o yaml | grep nodeName
      nodeName: worker-0

Label 2nd worker node

    oc label nodes worker-1 daemon=compute --overwrite
    node/worker-1 labeled

    oc get daemonset
    NAME                     DESIRED   CURRENT   READY   UP-TO-DATE   AVAILABLE   NODE SELECTOR    AGE
    nova-compute-daemonset   2         2         1       2            1           daemon=compute   25m

    oc get pods
    NAME                             READY   STATUS    RESTARTS   AGE
    nova-compute-daemonset-gjjnm     3/3     Running   0          2m48s
    nova-compute-daemonset-grb7j     3/3     Running   0          29s
    nova-operator-5d56d8459b-mbqrn   1/1     Running   0          10m

    oc get pods -o custom-columns='NAME:metadata.name,NODE:spec.nodeName'
    NAME                             NODE
    nova-compute-daemonset-gjjnm     worker-0
    nova-compute-daemonset-grb7j     worker-1
    nova-operator-5d56d8459b-mbqrn   worker-1

If need get into nova-compute container of daemonset via:

    oc exec -c nova-compute nova-compute-daemonset-gjjnm -i -t -- bash -il

## POST steps to add compute workers to the cell

#### Map the computes to the default cell
    (undercloud) $ source stackrc
    (undercloud) $ CTRL=controller-0
    (undercloud) $ CTRL_IP=$(openstack server list -f value -c Networks --name $CTRL | sed 's/ctlplane=//')
    (undercloud) $ export CONTAINERCLI='podman'
    (undercloud) $ ssh heat-admin@${CTRL_IP} sudo ${CONTAINERCLI} exec -i -u root nova_api  nova-manage cell_v2 discover_hosts --by-service --verbose
    Warning: Permanently added '192.168.24.44' (ECDSA) to the list of known hosts.
    Found 2 cell mappings.
    Skipping cell0 since it does not contain hosts.
    Getting computes from cell 'default': ba9981ae-1e79-4b20-a6ff-0416f986af3b
    Creating host mapping for service worker-0
    Creating host mapping for service worker-1
    Found 2 unmapped computes in cell: ba9981ae-1e79-4b20-a6ff-0416f986af3b

    (undercloud) $ ssh heat-admin@${CTRL_IP} sudo ${CONTAINERCLI} exec -i -u root nova_api  nova-manage cell_v2 list_hosts
    Warning: Permanently added '192.168.24.44' (ECDSA) to the list of known hosts.
    +-----------+--------------------------------------+------------------------+
    | Cell Name |              Cell UUID               |        Hostname        |
    +-----------+--------------------------------------+------------------------+
    |  default  | ba9981ae-1e79-4b20-a6ff-0416f986af3b | compute-0.redhat.local |
    |  default  | ba9981ae-1e79-4b20-a6ff-0416f986af3b | compute-1.redhat.local |
    |  default  | ba9981ae-1e79-4b20-a6ff-0416f986af3b |        worker-0        |
    |  default  | ba9981ae-1e79-4b20-a6ff-0416f986af3b |        worker-1        |
    +-----------+--------------------------------------+------------------------+

#### Create an AZ and add the OCP workers at it

    (undercloud) $ source overcloudrc
    (overcloud) $ openstack aggregate create --zone ocp ocp
    (overcloud) $ openstack aggregate add host ocp worker-0
    (overcloud) $ openstack aggregate add host ocp worker-1
    (overcloud) $ openstack availability zone list --compute --long
    +-----------+-------------+---------------+---------------------------+----------------+----------------------------------------+
    | Zone Name | Zone Status | Zone Resource | Host Name                 | Service Name   | Service Status                         |
    +-----------+-------------+---------------+---------------------------+----------------+----------------------------------------+
    | internal  | available   |               | controller-0.redhat.local | nova-conductor | enabled :-) 2020-01-20T13:54:35.000000 |
    | internal  | available   |               | controller-0.redhat.local | nova-scheduler | enabled :-) 2020-01-20T13:54:34.000000 |
    | nova      | available   |               | compute-1.redhat.local    | nova-compute   | enabled :-) 2020-01-20T13:54:40.000000 |
    | nova      | available   |               | compute-0.redhat.local    | nova-compute   | enabled :-) 2020-01-20T13:54:42.000000 |
    | ocp       | available   |               | worker-0                  | nova-compute   | enabled :-) 2020-01-20T13:54:32.000000 |
    | ocp       | available   |               | worker-1                  | nova-compute   | enabled :-) 2020-01-20T13:54:35.000000 |
    +-----------+-------------+---------------+---------------------------+----------------+----------------------------------------+

#### Check nova compute service shows as up on the worker nodes

    (overcloud) $ openstack compute service list -c Id -c Host -c Zone -c State
    +---------------------------+----------+-------+
    | Host                      | Zone     | State |
    +---------------------------+----------+-------+
    | controller-0.redhat.local | internal | up    |
    | controller-0.redhat.local | internal | up    |
    | compute-0.redhat.local    | nova     | up    |
    | compute-1.redhat.local    | nova     | up    |
    | worker-0                  | ocp      | up    |
    | worker-1                  | ocp      | up    |
    +---------------------------+----------+-------+


## INSTALL THE OVS AGENT OPERATOR BEFORE START AN INSTANCE!!


## Start an instance and verify network connectivity works

NOTE: selinux needs to be disable to start instance

    2020-01-21 10:28:12.280 164015 ERROR nova.compute.manager [instance: fd1cf110-3921-4a65-b45d-807709fe5008] libvirt.libvirtError: internal error: process exited while connecting to monitor: libvirt:  error : cannot execute binary /usr/libexec/qemu-kvm: Permission denied

### Create two instances

    (overcloud) $ openstack server create --flavor m1.small --image cirros --nic net-id=$(openstack network list --name private -f value -c ID) --availability-zone ocp test
    (overcloud) $ openstack server create --flavor m1.small --image cirros --nic net-id=$(openstack network list --name private -f value -c ID) --availability-zone ocp test2
    (overcloud) $ openstack server list --long -c ID -c Name -c Status -c "Power State" -c Networks -c Host
    +--------------------------------------+-------+--------+-------------+-----------------------+----------+
    | ID                                   | Name  | Status | Power State | Networks              | Host     |
    +--------------------------------------+-------+--------+-------------+-----------------------+----------+
    | 55eb5cef-2580-48b8-a3ee-d27e96979fac | test2 | ACTIVE | Running     | private=192.168.0.58  | worker-0 |
    | 516a6b9c-a88d-4718-96bc-83d4315249fc | test  | ACTIVE | Running     | private=192.168.0.117 | worker-1 |
    +--------------------------------------+-------+--------+-------------+-----------------------+----------+

### Check tenant network connectivity from inside the dhcp namespace 

    (undercloud) [stack@undercloud-0 ~]$ ssh heat-admin@192.168.24.44
    [heat-admin@controller-0 ~]$ sudo -i

#### Ping instances from inside the namespace

Note: If it fails it might be that you need to apply OpenStack security rules!

    [root@controller-0 ~]# ip netns exec qdhcp-3821b285-fcc4-485b-89c1-6a5d242e7742 sh
    sh-4.4# ping -c 1 192.168.0.58
    PING 192.168.0.58 (192.168.0.58) 56(84) bytes of data.
    64 bytes from 192.168.0.58: icmp_seq=1 ttl=64 time=1.07 ms

    --- 192.168.0.58 ping statistics ---
    1 packets transmitted, 1 received, 0% packet loss, time 0ms
    rtt min/avg/max/mdev = 1.074/1.074/1.074/0.000 ms

    sh-4.4# ping -c 1 192.168.0.117
    PING 192.168.0.117 (192.168.0.117) 56(84) bytes of data.
    64 bytes from 192.168.0.117: icmp_seq=1 ttl=64 time=5.05 ms

    --- 192.168.0.117 ping statistics ---
    1 packets transmitted, 1 received, 0% packet loss, time 0ms
    rtt min/avg/max/mdev = 5.051/5.051/5.051/0.000 ms

#### Login to one of the instances via ssh 

    sh-4.4# ssh cirros@192.168.0.58
    The authenticity of host '192.168.0.58 (192.168.0.58)' can't be established.
    RSA key fingerprint is SHA256:TYk+lqGQ3tqWgmCe7CnmoJXZ15MYXcF2ANnv0vWpNn0.
    Are you sure you want to continue connecting (yes/no/[fingerprint])? yes

    Warning: Permanently added '192.168.0.58' (RSA) to the list of known hosts.
    cirros@192.168.0.58's password:

#### From the instance ping the other one

    $ ping -c 1 192.168.0.117
    PING 192.168.0.117 (192.168.0.117): 56 data bytes
    64 bytes from 192.168.0.117: seq=0 ttl=64 time=4.434 ms

    --- 192.168.0.117 ping statistics ---
    1 packets transmitted, 1 packets received, 0% packet loss
    round-trip min/avg/max = 4.434/4.434/4.434 ms

## Cleanup

    oc delete -f deploy/crds/nova_v1alpha1_compute_cr.yaml
    oc delete -f deploy/operator.yaml
    oc delete -f deploy/role.yaml
    oc delete -f deploy/role_binding.yaml
    oc delete -f deploy/service_account.yaml
    oc delete -f deploy/crds/nova_v1alpha1_compute_crd.yaml
