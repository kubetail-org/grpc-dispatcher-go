load('ext://restart_process', 'docker_build_with_restart')

# example-server
local_resource(
  'example-server-compile',
  'cd example && CGO_ENABLED=0 GOOS=linux go build -o ../.tilt/server ./cmd/server/main.go',
  deps=[
    './example/cmd/server',
    './example/internal/examplepb'
  ]
)

docker_build_with_restart(
  'example-server',
  dockerfile='hack/tilt/Dockerfile.example-server',
  context='.',
  entrypoint="/server/server",
  only=[
    './.tilt/server',
  ],
  live_update=[
    sync('./.tilt/server', '/server/server'),
  ]
)

# example-app
local_resource(
  'example-app-compile',
  'cd example && CGO_ENABLED=0 GOOS=linux go build -o ../.tilt/app ./cmd/app/main.go',
  deps=[
    './go.mod',
    './go.sum',
    './balancer.go',
    './dispatcher.go',
    './options.go',
    './resolver.go',
    './example/cmd/app',
    './example/internal/app',
    './example/internal/examplepb'
  ]
)

docker_build_with_restart(
  'example-app',
  dockerfile='hack/tilt/Dockerfile.example-app',
  context='.',
  entrypoint="/app/app",
  only=[
    './.tilt/app',
    './example/templates'
  ],
  live_update=[
    sync('./.tilt/app', '/app/app'),
  ]
)

# apply manifests
k8s_yaml('hack/tilt/example-server.yaml')
k8s_yaml('hack/tilt/example-app.yaml')
k8s_yaml('hack/tilt/chaoskube.yaml')

# define resources
k8s_resource(
  'example-server',
  port_forwards='50051:50051'
)

k8s_resource(
  'example-app',
  objects=[
    'example-app:serviceaccount',
    'example-app:role',
    'example-app:rolebinding'
  ],
  port_forwards='4000:4000'
)

k8s_resource(
  'chaoskube',
  objects=[
    'chaoskube:serviceaccount',
    'chaoskube:clusterrole',
    'chaoskube:clusterrolebinding',
    'chaoskube:role',
    'chaoskube:rolebinding'
  ]
)
