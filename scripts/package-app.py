#!/usr/bin/env python3
"""Package a self-contained .app, signing nested Mach-O executables first."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
APP = ROOT / 'dist/DockerD.app'

def run(*args):
    subprocess.run(args, cwd=ROOT, check=True)

def main():
    binary_path = subprocess.check_output(['swift', 'build', '-c', 'release', '--show-bin-path'], cwd=ROOT, text=True).strip()
    APP.parent.mkdir(parents=True, exist_ok=True)
    app = Path(tempfile.mkdtemp(prefix='.DockerD-staging-', suffix='.app', dir=APP.parent))
    macos = app / 'Contents/MacOS'
    resources = app / 'Contents/Resources'
    macos.mkdir(parents=True)
    resources.mkdir(parents=True)
    shutil.copy2(Path(binary_path) / 'DockerD', macos / 'DockerD')
    shutil.copy2(ROOT / 'build/dockerd-service', macos / 'dockerd-service')
    shutil.copy2(ROOT / 'resources/Info.plist', app / 'Contents/Info.plist')
    shutil.copy2(ROOT / 'resources/runtime-lock.json', resources / 'runtime-lock.json')
    shutil.copytree(ROOT / '.runtime/bundle', resources / 'Runtime', symlinks=True)
    shutil.copytree(ROOT / 'resources/licenses', resources / 'Licenses')
    if (ROOT / 'docs/third-party.md').exists():
        shutil.copy2(ROOT / 'docs/third-party.md', resources / 'Third-party-notices.md')
    run('swift', str(ROOT / 'scripts/make-icon.swift'), str(ROOT / 'build/AppIcon.iconset'))
    run('iconutil', '-c', 'icns', str(ROOT / 'build/AppIcon.iconset'), '-o', str(resources / 'AppIcon.icns'))
    identity = os.environ.get('DOCKERD_SIGN_IDENTITY', '-')
    for path in sorted(app.rglob('*')):
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
    run(*args, str(app))
    run('codesign', '--verify', '--deep', '--strict', str(app))
    previous = APP.with_name('.DockerD-previous.app')
    if previous.exists():
        shutil.rmtree(previous)
    if APP.exists():
        APP.rename(previous)
    try:
        app.rename(APP)
    except Exception:
        if previous.exists():
            previous.rename(APP)
        raise
    if previous.exists():
        shutil.rmtree(previous)
    print('Ready: ' + str(APP))

if __name__ == '__main__':
    main()
