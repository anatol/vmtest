# VmTest QEMU image creation instructions

`VmTest` is capable to run tests as root inside a QEMU virtual machine. It needs a Linux kernel and initramfs binaries.
Here are instructions that help to create it.


## Build a linux kernel binary:

```shell script
make x86_64_defconfig
make kvm_guest.config
scripts/config -d MODULES
# here enable modules you are interested in e.g.
scripts/config -e DM_CRYPT -e CRYPTO_XTS
make -j20
# Now arch/x86/boot/bzImage contains the require binary. Copy it to your tests location.
cp arch/x86/boot/bzImage $YOUR_TESTS_LOCATION
```

## Build an Arch Linux rootfs

```shell script
dd if=/dev/zero of=rootfs.raw bs=1G count=1
mkfs.ext4 rootfs.raw
sudo losetup -fP rootfs.raw
mkdir rootfs
sudo mount /dev/loop0 rootfs
sudo pacstrap -c rootfs base openssh

echo "[Match]
Name=enp0s3

[Network]
DHCP=yes" | sudo tee rootfs/etc/systemd/network/20-wired.network

sudo sed -i '/^root/ { s/:x:/::/ }' rootfs/etc/passwd
sudo sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin yes/' rootfs/etc/ssh/sshd_config
sudo sed -i 's/#PermitEmptyPasswords no/PermitEmptyPasswords yes/' rootfs/etc/ssh/sshd_config

sudo arch-chroot rootfs systemctl enable sshd systemd-networkd
sudo rm rootfs/var/cache/pacman/pkg/*
sudo umount rootfs
sudo losetup -d /dev/loop0
rm -r rootfs
```

To keep `rootfs.raw` unmodifiable it is a good idea to create a CoW overlay:

```qemu-img create -o backing_file=rootfs.raw,backing_fmt=raw -f qcow2 rootfs.cow```

`rootfs.cow` will contain any your changes that you make on top of read-only `rootfs.raw`.

You can quickly verify that this image boots fine with

```
qemu-system-x86_64 \
  -drive file=rootfs.cow,index=0 \
  -net user,hostfwd=tcp::10022-:22 -net nic \
  -nographic \
  -kernel bzImage -append "console=ttyS0 root=/dev/sda rw debug earlyprintk=serial"\
  -enable-kvm -cpu host
```

Test that ssh works:
`ssh -p 10022 -o "StrictHostKeyChecking no" root@localhost`

To stop the QEMU instance press `Ctrl+A` then `X`.
