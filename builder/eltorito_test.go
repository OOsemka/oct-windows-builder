package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestPatchElToritoNoprompt(t *testing.T) {
	img := buildMinimalWindowsISO()
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(path, img, 0644); err != nil {
		t.Fatal(err)
	}
	if err := patchISONoprompt(dir); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	prompt, noprompt, catalogLBA, err := inspectWindowsISO(f)
	if err != nil {
		t.Fatal(err)
	}
	if noprompt.lba != 34 {
		t.Fatalf("noprompt lba=%d", noprompt.lba)
	}
	same, err := extentsEqual(f, prompt, noprompt)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Fatal("prompt boot image should match noprompt after patch")
	}
	sec, err := readSectorAt(f, catalogLBA)
	if err != nil {
		t.Fatal(err)
	}
	got := binary.LittleEndian.Uint32(sec[96+8 : 96+12])
	if got != noprompt.lba {
		t.Fatalf("catalog EFI LBA=%d want %d", got, noprompt.lba)
	}
}

func TestPatchElToritoIdempotent(t *testing.T) {
	img := buildMinimalWindowsISO()
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(path, img, 0644); err != nil {
		t.Fatal(err)
	}
	if err := patchISONoprompt(dir); err != nil {
		t.Fatal(err)
	}
	if err := patchISONoprompt(dir); err != nil {
		t.Fatal(err)
	}
}

func TestResolveISOPathFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(p, make([]byte, isoSectorSize+1), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveISOPath(dir)
	if err != nil || got != p {
		t.Fatalf("got %q err %v", got, err)
	}
}

func buildMinimalWindowsISO() []byte {
	const sectors = 48
	img := make([]byte, sectors*isoSectorSize)

	pvd := img[16*isoSectorSize : 17*isoSectorSize]
	pvd[0] = isoPVDType
	copy(pvd[1:], isoStdID)
	putDirRecord(pvd, 156, "\x00", 20, isoSectorSize, true)

	boot := img[17*isoSectorSize : 18*isoSectorSize]
	boot[0] = isoBootType
	copy(boot[1:], isoStdID)
	copy(boot[7:], "EL TORITO SPECIFICATION")
	binary.LittleEndian.PutUint32(boot[71:75], 25)

	term := img[18*isoSectorSize : 19*isoSectorSize]
	term[0] = isoTerminatorType
	copy(term[1:], isoStdID)

	root := img[20*isoSectorSize : 21*isoSectorSize]
	off := 0
	off = putDirRecord(root, off, "\x00", 20, isoSectorSize, true)
	off = putDirRecord(root, off, "\x01", 20, isoSectorSize, true)
	putDirRecord(root, off, "EFI", 21, isoSectorSize, true)

	efi := img[21*isoSectorSize : 22*isoSectorSize]
	off = 0
	off = putDirRecord(efi, off, "\x00", 21, isoSectorSize, true)
	off = putDirRecord(efi, off, "\x01", 20, isoSectorSize, true)
	putDirRecord(efi, off, "MICROSOFT", 22, isoSectorSize, true)

	ms := img[22*isoSectorSize : 23*isoSectorSize]
	off = 0
	off = putDirRecord(ms, off, "\x00", 22, isoSectorSize, true)
	off = putDirRecord(ms, off, "\x01", 21, isoSectorSize, true)
	putDirRecord(ms, off, "BOOT", 23, isoSectorSize, true)

	const fileSize = 4 * isoSectorSize
	bootDir := img[23*isoSectorSize : 24*isoSectorSize]
	off = 0
	off = putDirRecord(bootDir, off, "\x00", 23, isoSectorSize, true)
	off = putDirRecord(bootDir, off, "\x01", 22, isoSectorSize, true)
	off = putDirRecord(bootDir, off, "EFISYS.BIN;1", 30, fileSize, false)
	putDirRecord(bootDir, off, "EFISYS_NOPROMPT.BIN;1", 34, fileSize, false)

	cat := img[25*isoSectorSize : 26*isoSectorSize]
	cat[0] = 0x01
	cat[30] = 0x55
	cat[31] = 0xAA
	cat[32] = 0x88
	cat[64] = 0x90
	cat[65] = 0xEF
	cat[96] = 0x88
	binary.LittleEndian.PutUint16(cat[96+6:96+8], uint16(fileSize/512))
	binary.LittleEndian.PutUint32(cat[96+8:96+12], 30)

	for i := 0; i < fileSize; i++ {
		img[30*isoSectorSize+i] = 0xAA
		img[34*isoSectorSize+i] = 0xBB
	}
	return img
}

func putDirRecord(buf []byte, off int, name string, lba, size uint32, isDir bool) int {
	nb := []byte(name)
	recLen := 33 + len(nb)
	if recLen%2 == 1 {
		recLen++
	}
	buf[off] = byte(recLen)
	binary.LittleEndian.PutUint32(buf[off+2:off+6], lba)
	binary.BigEndian.PutUint32(buf[off+6:off+10], lba)
	binary.LittleEndian.PutUint32(buf[off+10:off+14], size)
	binary.BigEndian.PutUint32(buf[off+14:off+18], size)
	if isDir {
		buf[off+25] = 0x02
	}
	buf[off+28] = 1
	buf[off+31] = 1
	buf[off+32] = byte(len(nb))
	copy(buf[off+33:], nb)
	return off + recLen
}
