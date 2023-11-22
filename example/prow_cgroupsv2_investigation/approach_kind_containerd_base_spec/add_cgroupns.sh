key=$(cat unadjusted_kind_base_oci_spec.json | jq '.linux.namespaces | map(select(.type == "cgroup"))[0]')
if [ $key = "null" ]; then
    echo "Adding ";
  else
    echo "Not NULL";
fi

cat /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/example/tmp/approach_kind_containerd_base_spec/unadjusted_kind_base_oci_spec.json | jq '.linux.namespaces += [{
    "type": "cgroup"
}]' > ./tmp && cp ./tmp unadjusted_kind_base_oci_spec.json