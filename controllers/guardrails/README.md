# The Backend of Guardrails Orchestrator

* CustomResourceDefinition: The CRD defines the components of GuardrailsOrchestator which are the generator, chunker, and detector
    * Kind: `GuardrailsOrchestratorService`
    * Version: `v1alpha1`
* Controller: The controller reconciles the `GuadrailsOrchestratorSevice` custom resources, creates corresponding Pods, stores results, and cancels services.

## High Level Architecture


## State Transition of a GuardrailsOrchestrator

## Design

### Custom Resource Definition (CRD)
The data structure for a GuardrailsOrchestrator contains the following fields:

| GuardrailsOrchestrator | Data Type | Optional | Parameter in GuardrailsOrchestrator | Description
| --- | --- | --- | --- | -- |
| Generator | string | | --generator | Generator name or ID|
| Detector | string |  | --detector | Detector name or ID |
| DetectorArgs | []string |  | --detector_args | Configurations for the selected detector. The data is converted to a string in this format and passed to the GuardrailsOrchestrator: `arg1=val1,arg2=val2` |
| Chunker | string | ✅ | --chunker | Chunker name or ID |
| ChunkerArgs | []string | ✅ | --chunker_args | Configurations for the selected chunker. The data is converted to a string in this format and passed to the GuardrailsOrchestrator: `arg1=val1,arg2=val2` |

The `Status` subresource of the `GuardrailsOrchestrator` CRB contains the following information:

* `PodName`: the name of the Pod that runs the guardrails-orchestrator service
* `State`: records the status of the  guardrails-orchestrator service. Possible values are:
    * `New`: the service is created but not yet processed by the controller
    * `Scheduled`: a Pod is created by the controller for the service
    * `Running`: the Pod for the service is running
    * `Complete`: the service request finishes or fails
    * `Cancelled`: the controller canceled the service and will mark it as complete
* `Reason`: details about the current state.
    * `NoReason`: there is no information about the current state
    * `Succeeded`: the service finished successfully
    * `Failed`: the service failed
    * `Cancelled`: the service is cancelled
* `Message`: additional details about the final state
* `LastScheduleTime`: timestamp of when the Pod is scheduled
* `CompleteTime`: timestamp of when the service's state is `Complete`
* `Results`: stores the results of the guardrails-orchestrator service results

## The Controller
The controller is responsible for monitoring the `GuardrailsOrchestratorService` CRs and reconciling the corresponding Pods. Here are the details of how the controller handles an `GuardrailsOrchestratorService` CR:
* ConfigMap: provides the controller with instructions on how to configure the `GuardrailsOrchestrator` CR:
    * pod-image
    * pod-checking-interval
    * image-pull-policy

* Arguments: the controller supports the following command line arguments:
    * --namespace: the namespace where you deploy the controller. By default, the namespace of the controller deployment is used
    * --configmap: the name of the ConfigMap where the config settings are stored

* Finalizer
