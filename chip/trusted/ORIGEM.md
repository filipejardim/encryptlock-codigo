Certificates of the manufacturers of real security chips (TPM 2.0).

From Microsoft TrustedTpm.cab (https://go.microsoft.com/fwlink/?linkid=2097925), downloaded 8 Oct 2026:
hardware makers only (AMD, Atmel, Infineon, Intel, NationZ, Nuvoton, QC, STMicro).
The Microsoft folder was left out on purpose: Microsoft also signs virtual
chips (cloud servers, virtual machines); Microsoft Pluton is to be checked
before it is accepted.

Added: AMD/RootCA/AMDTPM-ftpm.cer - root of AMD firmware chips (fTPM, inside
the processor), from http://ftpm.amd.com/pki/aia/264D39A23CEB5D5B49D610044EEBD121
(SHA-1 E01AEF00737AB32F28F3374EB2EA72A682DE45C1). It is not in the cab.

11 Oct 2026 (security review):
- "Microsoft TPM Root Certificate Authority 2014" was inside the QC folder of
  the cab. It is a Microsoft root (it also certifies keys of chips Microsoft
  does not make in a factory), so it is NOT trusted: kept only for reference
  as QC/excluded-...cer.txt (not read). Qualcomm's own root stays.
- "Microsoft Pluton Root CA 2021" (in AMD/RootCA) is trusted only through the
  "Pluton Factory ... EK ICA" certificates (chip.go, plutonOK).
- The six older STMicro intermediates without "_2" have a malformed serial
  number that Go refuses; ST reissued them (the "_2" files), which are read.
