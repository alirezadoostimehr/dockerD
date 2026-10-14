#!/bin/sh
set -eu
: "${DOCKERD_NOTARY_PROFILE:?Set DOCKERD_NOTARY_PROFILE to your existing notarytool keychain profile}"
codesign --verify --deep --strict dist/DockerD.app
ditto -c -k --keepParent dist/DockerD.app dist/DockerD.zip
xcrun notarytool submit dist/DockerD.zip --keychain-profile "$DOCKERD_NOTARY_PROFILE" --wait
xcrun stapler staple dist/DockerD.app
