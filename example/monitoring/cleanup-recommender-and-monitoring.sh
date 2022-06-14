kubectl delete daemonset.apps/reserved-resources-recommender
kubectl delete priorityclass reserved-resources-recommender

kubectl delete clusterrole.rbac.authorization.k8s.io/prometheus-reserved-resources-recommender
kubectl delete clusterrolebinding.rbac.authorization.k8s.io/prometheus-reserved-resources-recommender
kubectl delete configmap/prometheus-server-conf-reserved-resources-recommender
kubectl delete deployment.apps/prometheus-deployment-reserved-resources-recommender 
kubectl delete service/prometheus-web-reserved-resources-recommender
kubectl delete service/reserved-resources-recommender
kubectl delete pvc prometheus-reserved-resources-recommender

kubectl delete configmap/grafana-dashboard-providers-reserved-resources-recommender
kubectl delete configmap/grafana-dashboards-reserved-resources-recommender 
kubectl delete configmap/grafana-datasources-reserved-resources-recommender 
kubectl delete deployment.apps/grafana-reserved-resources-recommender
kubectl delete service/grafana-reserved-resources-recommender