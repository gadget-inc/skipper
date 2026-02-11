variable "TAG" {
  default = "latest"
}

variable "REGISTRY" {
  default = ""
}

variable "PLATFORM" {
  default = "linux/amd64"
}

function "image_name" {
  params = [name]
  result = REGISTRY != "" ? "${REGISTRY}/${name}" : name
}

group "default" {
  targets = ["controller", "router"]
}

target "controller" {
  dockerfile = "Dockerfile"
  target     = "controller"
  tags       = ["${image_name("skipper-controller")}:${TAG}"]
  platforms  = [PLATFORM]
}

target "router" {
  dockerfile = "Dockerfile"
  target     = "router"
  tags       = ["${image_name("skipper-router")}:${TAG}"]
  platforms  = [PLATFORM]
}
