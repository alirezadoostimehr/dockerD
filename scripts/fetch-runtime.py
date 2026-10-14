#!/usr/bin/env python3
"""Download only locked artifacts. Never resolve 'latest' during a build."""
import concurrent.futures
import hashlib
import json
from pathlib import Path
import shutil
import tarfile
import tempfile
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / '.runtime' / 'cache'
DEST = ROOT / '.runtime' / 'bundle'

def fetch(entry):
    CACHE.mkdir(parents=True, exist_ok=True)
    target = CACHE / entry['name']
    def valid():
        return target.exists() and hashlib.file_digest(target.open('rb'), 'sha256').hexdigest() == entry['sha256']
    if not valid():
        print('Downloading ' + entry['name'], flush=True)
        part = target.with_suffix('.part')
        with urllib.request.urlopen(entry['url'], timeout=120) as response, part.open('wb') as output:
            shutil.copyfileobj(response, output)
        part.replace(target)
        if not valid():
            target.unlink()
            raise ValueError('Checksum mismatch: ' + entry['name'])
    return entry, target

def main():
    raw = (ROOT / 'resources/runtime-lock.json').read_bytes()
    manifest = json.loads(raw)
    fingerprint = hashlib.sha256(raw).hexdigest()
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        downloads = list(pool.map(fetch, manifest['host']))
    marker = DEST / '.manifest-sha256'
    if marker.exists() and marker.read_text() == fingerprint and (DEST / 'lima/bin/limactl').exists():
        print('Verified runtime bundle already prepared: ' + str(DEST))
        return
    # Never extract over an executing Mach-O file. macOS caches code signatures
    # by vnode and can SIGKILL an in-place rewritten binary even when bytes match.
    staging = Path(tempfile.mkdtemp(prefix='bundle-staging-', dir=DEST.parent))
    for entry, source in downloads:
        if entry.get('archive'):
            output = staging / entry['destination']
            output.mkdir(parents=True, exist_ok=True)
            with tarfile.open(source) as archive:
                # The artifacts are checksum-locked; also reject traversal links.
                for item in archive.getmembers():
                    p = (output / item.name).resolve()
                    if not p.is_relative_to(output.resolve()):
                        raise ValueError('Unsafe archive member: ' + item.name)
                    if item.issym() or item.islnk():
                        link = (p.parent / item.linkname).resolve()
                        if not link.is_relative_to(output.resolve()):
                            raise ValueError('Unsafe archive link: ' + item.name)
                archive.extractall(output, filter='data')
        else:
            output = staging / entry['destination']
            output.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, output)
            output.chmod(0o755)
    (staging / '.manifest-sha256').write_text(fingerprint)
    previous = DEST.with_name('bundle-previous')
    if previous.exists():
        shutil.rmtree(previous)
    if DEST.exists():
        DEST.rename(previous)
    try:
        staging.rename(DEST)
    except Exception:
        if previous.exists():
            previous.rename(DEST)
        raise
    if previous.exists():
        shutil.rmtree(previous)
    print('Verified runtime bundle: ' + str(DEST))

if __name__ == '__main__':
    main()
