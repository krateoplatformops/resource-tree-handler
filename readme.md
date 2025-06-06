# Resource Tree Handler

This service manages the resource trees for all the compositions installed.

## Summary

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [API](#api)
4. [Configuration](#configuration)

## Overview

This service monitors all Kubernetes events that it receives on the `/events` endpoint to find create/deleted compositions (the events are obtained through an [eventrouter](http://github.com/krateoplatformops/eventrouter/) registration). When a composition is created, it creates a resource tree by fetching all managed resources' statuses. The resource tree is then published on `/compositions/<composition_id>`. If a delete event happens, then the resource tree is deleted from the cache and will not be served on the `/compositions/<composition_id>` endpoint anymore. The `/refresh/<composition_id>` endpoint can refreshes a resource tree for a given composition_id and the `/list` endpoint returns the list of all composition_ids that have a resource tree available. Additionally, the resource-tree-handler awaits sse events from the [eventsse](http://github.com/krateoplatformops/eventsse/) service, updating each object in the resource tree individually, when it has an event that notifies an update. Finally, it updates the status of the CR CompositionReference (i.e., the one that contains the filters) with the overall status of the composition, also setting the CompositionReference as the root of the resource tree.

> [!NOTE]  
> The `CompositionReference` is mandatory, if it is not present, the resource-tree-handler will not build the resource tree. Filters are optional.

> [!NOTE]  
> Every resource tree is refreshed completely every 8 hours.

## Architecture

![Resource Tree Handler](_diagrams/architecture.png)

![Resource Tree Handler sequence diagram](_diagrams/sequence_diagram.png)

## API

This service has five endpoints: 
- GET `/`: answers to health probes
- POST `/events`: receives events from the [eventrouter](http://github.com/krateoplatformops/eventrouter/)
- POST `/refresh/<composition_id>`: rebuilds the resource tree from scratch for the specified composition_id and json object reference. For example, with CURL:
  ```
  curl -X POST "http://resource-tree-handler.krateo-system:8086/refresh/7c10e572-3cb7-4815-9c47-a34d921e0f60" \
   -H 'Content-Type: application/json' \
   -d '{"apiVersion":"composition.krateo.io/v1-1-6","resource":"fireworksapps", "name":"demo4", "namespace":"fireworksapp-system"}'
  ```
- GET `/composition/<composition_id>`: returns the resource tree for the specified composition_id
- GET `/list`: returns a list of all the composition_ids that have a resource tree available

## Configuration
This webservice can be installed with the respective [HELM chart](http://github.com/krateoplatformops/resource-tree-handler-chart).

You will need to configure the [eventrouter](http://github.com/krateoplatformops/eventrouter) CR to forward events to the resource-tree-handler:
```yaml
apiVersion: eventrouter.krateo.io/v1alpha1
kind: Registration
metadata:
  name: resource-tree-handler
  namespace: krateo-system
spec:
  serviceName: resource-tree-handler
  endpoint: http://resource-tree-handler.krateo-system:8086/handle
```
This CR is automatically installed by the [HELM chart](http://github.com/krateoplatformops/resource-tree-handler-chart).

To filter objects from the resource tree, you should use the CompositionReferece Custom Resource Definition. To map the custom resource to the composition, four labels need to be added with the information of the composition:
 - `krateo.io/composition-group`
 - `krateo.io/composition-installed-version`
 - `krateo.io/composition-namespace`
 - `krateo.io/composition-name`

You can put a set of filters to exclude some resources from the resource tree. Each of `apiVersion`, `resource`, and `name` is evaluated independetly, and all must be true to filter a given resource. A field of the filter is true if the following criterias are met:
 - the field is missing or empty;
 - the field perfectly matches the resource;
 - the field is a regex and there is a match in the resource.

```yaml
apiVersion: resourcetrees.krateo.io/v1
kind: CompositionReference
metadata:
  name: fireworksapp-devcomm
  namespace: fireworksapp-system
  labels:
    krateo.io/composition-group: composition.krateo.io
    krateo.io/composition-version: v1-1-6
    krateo.io/composition-name: devcomm
    krateo.io/composition-namespace: fireworksapp-system
spec:
  filters:
    exclude:
    - apiVersion: "templates.krateo.io/v1alpha1"
      resource: "collections"
    - apiVersion: "templates.krateo.io/v1alpha1"
      resource: "widgets"
    - apiVersion: "v1"
      resource: "configmaps"
      name: "^composition-"
status:
  ...
```

The filters are evaluated at runtime, so changes made to the custom resource while the resource-tree-handler is running will be applied at the next event that triggers an update of the resource tree. The changed filter will trigger an update of the whole resource tree, equivalent to calling the `/refresh/<composition_id>` endpoint.

Further configuration will be needed in the HELM chart to include the url for the [eventsse](http://github.com/krateoplatformops/eventsse/), to receive the sse notifications for available events (default value is already set, but if you modify the [eventsse](http://github.com/krateoplatformops/eventsse/) service, the HELM chart needs to be updated).