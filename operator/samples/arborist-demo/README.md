# Arborist Demo Instructions

```bash
git clone git@github.com:gflarity/grove.git
cd grove
git pull && git checkout arborist

# Make sure your git repo is clean (e2e-cluster-up errrors for some reason)
# Also Python 3.14 known to break depdendencies, use 3.12
python3.12 -m venv /tmp/.venv
source /tmp/.venv/bin/activate
cd operator
pip install -r 2`1
make tidy # just incase

# the first time it might take a while and timeout while waiting for nodes
# just run it again
make e2e-cluster-up 

python hack/e2e-cluster/config-cluster.py --fake-gpu=yes
./samples/arborist-demo/setup-fake-gpus.sh
# configure the fake GPU oeprator with fake H200 and B200

# basic demo with topology with topology and GPUS
kubectl apply -f samples/arborist-demo/base/topology-gpu-pcs.yaml

cd ../arborist/cmd/arborist
go mod tidy
go build

# setup kubectl grove support, this assumes ~/.local/bin exists and is your $PATH
# export PATH=${PATH}:$HOME/.local/bin
ln -s ${PWD}/arborist ~/.local/bin/kubectl-grove

# Arborist, aka kubectl grove is our soon to be released tooling that will be available via krew for managing your Grove based infrastructure, such as podcliquesets. 

#First let's see what we're working with
kubectl get nodes

# Ask you can see there around 30 nodes in this cluster.  Let's describe a node.
kubectl describe node k3d-shared-e2e-test-cluster-agent-0 | less

#As you see there are labels on this node helping to define the topology in use in this cluster.

# Now let's start up some workloads. 
kubectl apply -f operator/samples/arborist-demo/base/topology-gpu-pcs.yaml

# This was a topology enabled grove podcliqueset with 2 podcliquetset replicas. Here's what it looks like:
less operator/samples/arborist-demo/base/topology-gpu-pcs.yaml

# Next let's deployment a podcliqueset with a bug in it. There is a typo in one of the docker images.  
kubectl apply -f operator/samples/arborist-demo/errors/group1-image/01-typo-registry-with-scaling-group.yaml

# Let's take a look at it
less operator/samples/arborist-demo/errors/group1-image/01-typo-registry-with-scaling-group.yaml

# Finally I'll add some non-grove GPU deployments, just to provide some background noise (this will make sense in a little bit)
kubectl apply -f operator/samples/arborist-demo/non-grove/topology-gpu-deployments.yaml

# Now let's run arborist, available conveniently via kubectl rove.
kubectl grove


# If TUI isn't your jam, we're also going to be making plain cli tooling as well. 
kubectl grove topology block

kubectl grove topology rack

# Finally if you think you've encountered a bug, or just need some help diagnosing an issue you're having, the diagnostic command helps us, help you:
#

tar xzf 

# We have many more features planned and would love your input. Please submit feedback and suggestion improvements at https://github.com/ai-dynamo/grove
```

