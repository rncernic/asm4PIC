# asm4PIC

**asm4PIC** is a simple command-line assembler for **Microchip PIC12** and **PIC16** microcontrollers.  
It converts assembly source files (`.asm`) into Intel HEX (`.hex`) files suitable for programming your device.  
Currently tested with **PIC16F687** and **PIC16F886**.

---

## Overview

`asm4PIC` assembles human-readable assembly code into machine instructions specific to each PIC microcontroller.  
It uses device configuration files (JSON format) that describe:
- Instruction set
- Program memory size
- Register file map
- Fuse bits and configuration options
- etc

This design makes it **easily extensible** to new PIC models by simply adding configuration files.

---

## Command-Line Usage

-asm string
        Path to the input assembly (.asm) file (required)
-config-dir string
        Directory containing microcontroller JSON config files (default "./configs")
-hex string
        Path to the output HEX file (defaults to <asm-file-name>.hex)
-mcu string
        Target microcontroller name, e.g., 'PIC16F687' (required)
-report string
        Path to the output assembly report file (defaults to printing to console)
