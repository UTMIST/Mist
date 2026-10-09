#!/usr/bin/python3
"""Online sparse expansion of the known Mist loop image; preserves existing data."""
import argparse
import os
from pathlib import Path
import stat
import subprocess

GIB = 1024**3
image = Path('/srv/mist-storage-volume.img')
root = Path('/srv/mist-storage')
def run(*args): return subprocess.check_output(args,text=True,stderr=subprocess.STDOUT).strip()

parser = argparse.ArgumentParser()
parser.add_argument('--gib',type=int,default=1600)
args = parser.parse_args()
if os.geteuid()!=0: raise SystemExit('Requires the actual QuietBox host namespace')
if image.is_symlink() or not stat.S_ISREG(image.stat().st_mode): raise SystemExit('Unknown image')
if run('blkid','-s','LABEL','-o','value',str(image))!='mist-shared': raise SystemExit('Unknown filesystem label')
device = run('findmnt','-n','-o','SOURCE','--mountpoint',str(root))
if Path(run('losetup','-n','-O','BACK-FILE',device))!=image: raise SystemExit('Unknown backing device')
size=args.gib*GIB
if not 100<=args.gib<=2**20 or size<image.stat().st_size: raise SystemExit('Invalid size or attempted shrink')
fs=os.statvfs(image.parent)
reserved=image.stat().st_blocks*512
if size-reserved > fs.f_bavail*fs.f_frsize-200*GIB: raise SystemExit('Insufficient host space after 200 GiB reserve')
# Capacity is sparse: reserve no large amount of host space in advance.
# The guard above checks that the requested capacity could fit with host headroom.
with image.open('r+b') as backing:
    backing.truncate(size)
subprocess.run(['losetup','-c',device],check=True)
subprocess.run(['resize2fs',device],check=True)
print('Known shared image expanded without bulk preallocation:',args.gib,'GiB')
subprocess.run(['df','-h',str(root),'/'],check=True)
