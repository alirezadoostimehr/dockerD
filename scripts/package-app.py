#!/usr/bin/env python3
"""Package a self-contained .app, signing nested Mach-O executables first."""
import os
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parent.parent
APP = ROOT / 'dist/DockerD.app'

def run(*args):
    subprocess.run(args, cwd=ROOT, check=True)

def main():
    binary_path = subprocess.check_output(['swift', 'build', '-c', 'release', '--show-bin-path'], cwd=ROOT, text=True).strip()
    if APP.exists():
        shutil.rmtree(APP)
    macos = APP / 'Contents/MacOS'
    resources = APP / 'Contents/Resources'
    macos.mkdir(parents=True)
    resources.mkdir(parents=True)
    shutil.copy2(Path(binary_path) / 'DockerD', macos / 'DockerD')
    shutil.copy2(ROOT / 'build/dockerd-service', macos / 'dockerd-service')
    shutil.copy2(ROOT / 'resources/Info.plist', APP / 'Contents/Info.plist')
    shutil.copy2(ROOT / 'resources/runtime-lock.json', resources / 'runtime-lock.json')
    shutil.copytree(ROOT / '.runtime/bundle', resources / 'Runtime', symlinks=True)
    if (ROOT / 'docs/third-party.md').exists():
        shutil.copy2(ROOT / 'docs/third-party.md', resources / 'Third-party-notices.md')
    run('swift', str(ROOT / 'scripts/make-icon.swift'), str(ROOT / 'build/AppIcon.iconset'))
    run('iconutil', '-c', 'icns', str(ROOT / 'build/AppIcon.iconset'), '-o', str(resources / 'AppIcon.icns'))
    identity = os.environ.get('DOCKERD_SIGN_IDENTITY', '-')
    for path in sorted(APP.rglob('*')):
        if not path.is_file() or path.is_symlink():
            continue
        with path.open('rb') as f:
            magic = f.read(4)
        if magic not in [b'\xcf\xfa\xed\xfe', b'\xfe\xed\xfa\xcf', b'\xca\xfe\xba\xbe']:
            continue
        args = ['codesign', '--force', '--sign', identity]
        if identity != '-':
            args += ['--options', 'runtime', '--timestamp']
        if path.name == 'limactl':
            args += ['--entitlements', str(ROOT / 'resources/virtualization.entitlements')]
        run(*args, str(path))
    args = ['codesign', '--force', '--sign', identity]
    if identity != '-':
        args += ['--options', 'runtime', '--timestamp']
    run(*args, str(APP))
    run('codesign', '--verify', '--deep', '--strict', str(APP))
    print('Ready: ' + str(APP))

if __name__ == '__main__':
    main()
