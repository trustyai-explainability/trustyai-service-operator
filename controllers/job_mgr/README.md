## Instructions to test the controller on Kind locally
When Job Manager is an enabled service LMevalJob requires `kueue.x-k8s.io/queue-name` label. 
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
1. Quota and Node Affinity example. We will create 5 jobs.
    
    Jobs labeled with `user-queue` will be run on `kueue-worker` node.
    Job labeled with `user-queue-2` will be run on `kueue-worker2` node.
    Job will be Suspended if there is not enough quota. 
    
    Run 3 times.
    ```bash
    cat <<EOF | kubectl create -f -
    apiVersion: trustyai.opendatahub.io/v1alpha1
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
    cat <<EOF | kubectl create -f -
    apiVersion: trustyai.opendatahub.io/v1alpha1
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
    
    # Each lmevaljob is represented by a Kueue Workload resource. A Workload is only ADMITTED when there is enough quota in a Queue. In our example, user-queue has 4 cpu quota. We created 3 jobs each requests 2 cpu; therefore only 2 jobs can be admitted to user-queue.

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

1. Preemption example:
    
    Clean up jobs
    ```
    kubectl delete lmevaljob $(kubectl get lmevaljob|grep evaljob-sample-|cut -d" " -f1)
    ```

    Create a new ClusterQueue, LocalQueue, and 2 WorkloadPriorityClass(low and high).
 
    ```bash
    cat <<EOF | kubectl apply -f -
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: ClusterQueue
    metadata:
      name: "cluster-queue-3"
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
            nominalQuota: 88Gi
        - name: "default-flavor-2"
          resources:
          - name: "cpu"
            nominalQuota: 4
          - name: "memory"
            nominalQuota: 88Gi
      preemption:
        withinClusterQueue: LowerPriority
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: LocalQueue
    metadata:
      namespace: "default"
      name: "user-queue-3"
    spec:
      clusterQueue: "cluster-queue-3"
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: WorkloadPriorityClass
    metadata:
      name: low-priority
    value: 10
    description: "10 is lower priority"
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: WorkloadPriorityClass
    metadata:
      name: high-priority
    value: 10000
    description: "10000 is higher priority"
    EOF
    ```

    Create 4 low priory jobs.
    Run 4 times.
    ```bash
    cat << EOF| kubectl create -f -
    apiVersion: trustyai.opendatahub.io/v1alpha1
    kind: LMEvalJob
    metadata:
      labels:
        app.kubernetes.io/name: fms-lm-eval-service
        app.kubernetes.io/managed-by: kustomize
        kueue.x-k8s.io/queue-name: user-queue-3
        kueue.x-k8s.io/priority-class: low-priority
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

    Verify they are in running state:
    ```
    NAME                                                     STATE
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-8cr8k   Running
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-n5s9d   Running
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-wnm2q   Running
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-xck8c   Running

    NAME                                                           QUEUE          RESERVED IN       ADMITTED   FINISHED   AGE
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-8cr8k-34feb   user-queue-3   cluster-queue-3   True                  22s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-n5s9d-1daba   user-queue-3   cluster-queue-3   True                  20s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-wnm2q-52093   user-queue-3   cluster-queue-3   True                  21s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-xck8c-44e13   user-queue-3   cluster-queue-3   True                  23s

    NAME                       READY   STATUS    RESTARTS   AGE   IP            NODE            NOMINATED NODE   READINESS GATES
    pod/evaljob-sample-8cr8k   1/1     Running   0          22s   10.244.1.17   kueue-worker    <none>           <none>
    pod/evaljob-sample-n5s9d   1/1     Running   0          20s   10.244.2.11   kueue-worker2   <none>           <none>
    pod/evaljob-sample-wnm2q   1/1     Running   0          21s   10.244.2.10   kueue-worker2   <none>           <none>
    pod/evaljob-sample-xck8c   1/1     Running   0          23s   10.244.1.16   kueue-worker    <none>           <none>
    ```


    Create 1 high priority job
    ```bash
    cat << EOF| kubectl create -f -
    apiVersion: trustyai.opendatahub.io/v1alpha1
    kind: LMEvalJob
    metadata:
      labels:
        app.kubernetes.io/name: fms-lm-eval-service
        app.kubernetes.io/managed-by: kustomize
        kueue.x-k8s.io/queue-name: user-queue-3
        kueue.x-k8s.io/priority-class: high-priority
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

    Job labeled with low-priority will be preempted/evicted(Suspended) by the new job labeled with high-priority because nominal cpu quota has reached.
    ```
    NAME                                                     STATE
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-8cr8k   Suspended
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-mqj8j   Running
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-n5s9d   Running
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-wnm2q   Running
    lmevaljob.trustyai.opendatahub.io/evaljob-sample-xck8c   Running

    NAME                                                           QUEUE          RESERVED IN       ADMITTED   FINISHED   AGE
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-8cr8k-34feb   user-queue-3                     False                 78s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-mqj8j-fdceb   user-queue-3   cluster-queue-3   True                  16s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-n5s9d-1daba   user-queue-3   cluster-queue-3   True                  76s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-wnm2q-52093   user-queue-3   cluster-queue-3   True                  77s
    workload.kueue.x-k8s.io/lmevaljob-evaljob-sample-xck8c-44e13   user-queue-3   cluster-queue-3   True                  79s

    NAME                       READY   STATUS    RESTARTS   AGE   IP            NODE            NOMINATED NODE   READINESS GATES
    pod/evaljob-sample-mqj8j   1/1     Running   0          15s   10.244.1.18   kueue-worker    <none>           <none>
    pod/evaljob-sample-n5s9d   1/1     Running   0          76s   10.244.2.11   kueue-worker2   <none>           <none>
    pod/evaljob-sample-wnm2q   1/1     Running   0          77s   10.244.2.10   kueue-worker2   <none>           <none>
    pod/evaljob-sample-xck8c   1/1     Running   0          79s   10.244.1.16   kueue-worker    <none>           <none>
    ```
