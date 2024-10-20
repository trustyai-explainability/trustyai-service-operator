## Instructions to test the controller on Kind locally

1. Setup Kind

    Create a kind cluster with 3 nodes

    ```bash
    cat <<EOF | kind create cluster --name kueue --config -
    kind: Cluster
    apiVersion: kind.x-k8s.io/v1alpha4
    nodes:
    - role: control-plane
    - role: worker
    - role: worker
    EOF
    ```

1. Setup Kueue

    This command install kueue with lmevaljob support:
    ```bash
    curl -L  https://github.com/kubernetes-sigs/kueue/releases/download/v0.8.1/manifests.yaml | sed 's/#  externalFrameworks/  externalFrameworks/; s/#  - "Foo.v1.example.com"/  - "trustyai.opendatahub.io\/lmevaljob"/'|kubectl apply --server-side -f -
    ```

    Create 2 sets of Kueue CRs.
    ```bash
    cat <<EOF | kubectl apply -f -
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: ClusterQueue
    metadata:
      name: "cluster-queue"
    spec:
      namespaceSelector: {} # match all.
      resourceGroups:
      - coveredResources: ["cpu", "memory"]
        flavors:
        - name: "default-flavor"
          resources:
          - name: "cpu"
            nominalQuota: 4
          - name: "memory"
            nominalQuota: 88888888Gi
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: ClusterQueue
    metadata:
      name: "cluster-queue-2"
    spec:
      namespaceSelector: {} # match all.
      resourceGroups:
      - coveredResources: ["cpu", "memory"]
        flavors:
        - name: "default-flavor-2"
          resources:
          - name: "cpu"
            nominalQuota: 4
          - name: "memory"
            nominalQuota: 50Gi
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: ResourceFlavor
    metadata:
      name: "default-flavor"
    spec:
      nodeLabels:
        kubernetes.io/hostname: kueue-worker
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: ResourceFlavor
    metadata:
      name: "default-flavor-2"
    spec:
      nodeLabels:
        kubernetes.io/hostname: kueue-worker2
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: LocalQueue
    metadata:
      namespace: "default"
      name: "user-queue"
    spec:
      clusterQueue: "cluster-queue"
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: LocalQueue
    metadata:
      namespace: "default"
      name: "user-queue-2"
    spec:
      clusterQueue: "cluster-queue-2"
    EOF
    ```



1. Install TAS CRDs
    ```bash
    make install
    ```

1. Start controllers locally
    First manually create the `trustyai-service-operator-config` configmap
    ```bash
    cat <<EOF | kubectl apply -f -
    apiVersion: v1
    kind: ConfigMap
    metadata:
    name: trustyai-service-operator-config
    labels:
        app.kubernetes.io/part-of: trustyai
    annotations:
        internal.config.kubernetes.io/generatorBehavior: unspecified
        internal.config.kubernetes.io/prefixes: trustyai-service-operator-
        internal.config.kubernetes.io/previousKinds: ConfigMap,ConfigMap
        internal.config.kubernetes.io/previousNames: config,trustyai-service-operator-config
        internal.config.kubernetes.io/previousNamespaces: default,default
    data:
        kServeServerless: disabled
        lmes-default-batch-size: "8"
        lmes-driver-image: quay.io/yhwang/ta-lmes-driver:latest
        lmes-grpc-port: "8082"
        lmes-grpc-service: lmes-grpc
        lmes-image-pull-policy: Always
        lmes-max-batch-size: "24"
        lmes-pod-checking-interval: 10s
        lmes-pod-image: quay.io/tedchang/ta-lmes-job:latest
        oauthProxyImage: quay.io/openshift/origin-oauth-proxy:4.14.0
        trustyaiOperatorImage: quay.io/tedchang/trustyai-service-operator:latest
        trustyaiServiceImage: quay.io/trustyai/trustyai-service:latest
    EOF
    ```
    Start the controller locally:
    ```bash
    ENABLED_SERVICES=LMES,JOB_MGR make run
    ```
    Verify logs
    ```
    INFO    Starting workers        {"controller": "lmevaljob", "controllerGroup": "trustyai.opendatahub.io", "controllerKind": "LMEvalJob", "worker count": 1}
    INFO    Starting workers        {"controller": "LMEvalJobWorkload", "controllerGroup": "trustyai.opendatahub.io", "controllerKind": "LMEvalJob", "worker count": 1}
    ```
1. Create 5 jobs. 
    
    Jobs labeled with `user-queue` will be run on `kueue-worker` node. Job labeled with `user-queue-2` will be run on `kueue-worker2` node.
    
    Run 3 times.
    ```bash
    cat <<EOF | kubectl apply -f -
    kind: LMEvalJob
    metadata:
    labels:
        app.kubernetes.io/name: fms-lm-eval-service
        app.kubernetes.io/managed-by: kustomize
        kueue.x-k8s.io/queue-name: user-queue
    generateName: evaljob-sample-
    namespace: default
    spec:
    pod:
        container:
        resources:
            requests:
            cpu: 2
    suspend: true
    model: hf
    modelArgs:
    - name: pretrained
        value: EleutherAI/pythia-70m
    taskList:
        taskNames:
        - unfair_tos
    logSamples: true
    limit: "5"
    EOF
    ```

    Run 2 times.
    ```bash
    cat <<EOF | kubectl apply -f -
    kind: LMEvalJob
    metadata:
    labels:
        app.kubernetes.io/name: fms-lm-eval-service
        app.kubernetes.io/managed-by: kustomize
        kueue.x-k8s.io/queue-name: user-queue-2
    generateName: evaljob-sample-
    namespace: default
    spec:
    pod:
        container:
        resources:
            requests:
            cpu: 2
    suspend: true
    model: hf
    modelArgs:
    - name: pretrained
        value: EleutherAI/pythia-70m
    taskList:
        taskNames:
        - unfair_tos
    logSamples: true
    limit: "5"
    EOF
    ```

1. Verify
    ```bash
    watch -d -n5 kubectl get lmevaljob,workloads,pod -owide
    ```
    
    ```
    # Initially you should see 5 lmevaljobs. One of the lmevaljob should be Suspended because not enough quota in the `user-queue` to admit the job. 
    NAME                                                     STATE
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-2zwb4   Suspended
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-678xz   Running
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-6gh6f   Running
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-d2jtx   Running  
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-dpr2q   Running
    
    # Each lmevaljob is represented by a Kueue Workerload resource. A Workload is only ADMITTED when there is enough quota in a Queue. In our example, user-queue has 4 cpu quota. We created 3 jobs each requests 2 cpu; therefore only 2 jobs can be admitted to user-queue.

    NAME                                                           QUEUE          RESERVED IN       ADMITTED   FINISHED   AGE
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-2zwb4-74b05   user-queue                                             71s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-678xz-bbc11   user-queue     cluster-queue     True                  72s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-6gh6f-0af0b   user-queue     cluster-queue     True                  82s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-d2jtx-fc24a   user-queue-2   cluster-queue-2   True                  13s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-dpr2q-ca946   user-queue-2   cluster-queue-2   True                  16s

    # Pod will not be created for Suspended job so we see only 4 Pods.

    NAME                       READY   STATUS    RESTARTS   AGE   IP            NODE            NOMINATED NODE   READINESS GATES
    pod/evaljob-sample-678xz   1/1     Running   0          72s   10.244.1.27   kueue-worker    <none>           <none>
    pod/evaljob-sample-6gh6f   1/1     Running   0          82s   10.244.1.26   kueue-worker    <none>           <none>
    pod/evaljob-sample-d2jtx   1/1     Running   0          13s   10.244.2.38   kueue-worker2   <none>           <none>
    pod/evaljob-sample-dpr2q   1/1     Running   0          16s   10.244.2.37   kueue-worker2   <none>           <none>
    ```