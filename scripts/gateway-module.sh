#!/usr/bin/env bash
# Before independent module releases, build with a temporary module file. Never
# commit local replace directives or relax public checksum verification globally.
prepare_gateway_module(){
  local repository_root=$1 temporary_directory=$2
  gramstack_gateway_root="$repository_root/services/gateway"
  gramstack_gateway_modfile="$temporary_directory/gateway.mod"
  cp "$gramstack_gateway_root/go.mod" "$gramstack_gateway_modfile"
  cp "$gramstack_gateway_root/go.sum" "$temporary_directory/gateway.sum"
  GOWORK=off go -C "$gramstack_gateway_root" mod edit -modfile="$gramstack_gateway_modfile" \
    -replace="github.com/theflywheel/GRAMStack/adapters/auth/oidc=$repository_root/adapters/auth/oidc"
}
gateway_go(){
  local operation=$1
  shift
  GOWORK=off go -C "$gramstack_gateway_root" "$operation" -modfile="$gramstack_gateway_modfile" "$@"
}

prepare_product_module(){
  local repository_root=$1 temporary_directory=$2
  gramstack_product_root="$repository_root/apps/sakhicredit"
  gramstack_product_modfile="$temporary_directory/product.mod"
  cp "$gramstack_product_root/go.mod" "$gramstack_product_modfile"
  cp "$gramstack_product_root/go.sum" "$temporary_directory/product.sum"
  GOWORK=off go -C "$gramstack_product_root" mod edit -modfile="$gramstack_product_modfile" \
    -replace="github.com/theflywheel/GRAMStack/sdk/go/client=$repository_root/sdk/go/client"
}
product_go(){
  local operation=$1
  shift
  GOWORK=off go -C "$gramstack_product_root" "$operation" -modfile="$gramstack_product_modfile" "$@"
}
