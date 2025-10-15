# asm4PIC

Simple assembler for PIC12 and 16 families. Tested with PIC16F687 and PIC16F886

Usage:
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
