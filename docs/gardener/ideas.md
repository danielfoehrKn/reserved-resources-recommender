# Ideal World
What k8s should have a Scheduler that schedules based on actual node utilisation.
We should be able to set a target average utilisation for nodes with a safety margin.
The scheduler then tries to schedule accordingly. 

**Why**: pod memory/cpu requests are usually accurate, but can give a hint. 
Average utilisation can be another scheduling factor.

Scheduler considers constraints such as
- CIDR range that allows only certain amount of pods
- Max pods: should be configurable arbitrarly based on whether the node still has resources: https://github.com/kubernetes/kubernetes/issues/23349
- Limits of external components 
  - Container runtime: PLEG issues with older docker runtime versions that could not handle many containers
  - Kubelet - probing, state-of-the world, ...
  -> many of those ccan b e mitigated by using the reserved resourcec reconciler that 
    gives the non-pod processes the resources they need (higher CPU shares, lower kubepods memory limit)