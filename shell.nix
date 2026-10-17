{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
  packages = with pkgs; [
    go
    nodejs
    python3
    k3s
    kubectl
  ];
}
