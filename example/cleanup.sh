kubectl delete daemonset.apps/better-resource-reservations
kubectl delete priorityclass better-resource-reservations

kubectl delete clusterrole.rbac.authorization.k8s.io/prometheus-better-resource-reservations
kubectl delete clusterrolebinding.rbac.authorization.k8s.io/prometheus-better-resource-reservations
kubectl delete configmap/prometheus-server-conf 
kubectl delete deployment.apps/prometheus-deployment-better-resource-reservations 
kubectl delete service/prometheus-web 
kubectl delete service/better-resource-reservations 

kubectl delete configmap/grafana-dashboard-providers 
kubectl delete configmap/grafana-dashboards-better-resource-reservations 
kubectl delete configmap/grafana-datasources-better-resource-reservations 
kubectl delete deployment.apps/grafana 
kubectl delete service/grafana 