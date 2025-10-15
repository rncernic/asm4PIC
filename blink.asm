    ; LED Blink Example for PIC16F886 (RA0)
    
    #DEFINE LED_PIN RA0
    #DEFINE LON BSF PORTA, RA0
    #DEFINE LOFF BCF PORTA, RA0
    
    BANK0   MACRO
                BCF STATUS, RP0
                BCF STATUS, RP1
            ENDM
            
    BANK1   MACRO
                BSF STATUS, RP0
                BCF STATUS, RP1
            ENDM
    
    BANK2   MACRO
                BCF STATUS, RP0
                BSF STATUS, RP1
            ENDM
    
    BANK3   MACRO
                BSF STATUS, RP0
                BSF STATUS, RP1
            ENDM
            
    
    LEDON	MACRO
            INIT:
                BSF		PORTA, LED_PIN
            ENDM
    
    __CONFIG _FOSC_INTOSCIO & _WDTE_OFF & _LVP_OFF & _CP_OFF & _CPD_OFF ; Internal RC, WDT off, LVP off, CP off, CPD off
    ORG 0x000
    GOTO    INIT            ; Go to initialization routine

    ; --- Main Program ---
    MAIN_LOOP:
        ORG		0x005			; Should start at 0x005 to skip interrupt vector at 0x004
        ;BSF PORTA, RA0			; Turn LED ON (Set RA0 high)
        LEDON
        CALL    DELAY_500MS     ; Delay
        ;BCF     PORTA, RA0      ; Turn LED OFF (Clear RA0 low)
        LOFF
        CALL    DELAY_500MS     ; Delay
        GOTO    MAIN_LOOP       ; Loop indefinitely

    ; --- Initialization Routine ---
    INIT:
        ; Configure RA0 as digital I/O (ANSEL/ANSELH)
        ;BCF     STATUS, RP1     ; Select Bank 0 (RP1=0, RP0=0)
        ;BCF     STATUS, RP0
        BANK2
        CLRF    ANSEL           ; Ensure all ANSEL bits are cleared for digital I/O
        CLRF    ANSELH          ; Ensure all ANSELH bits are cleared for digital I/O

        ; Configure RA0 as output (TRISA)
        ;BSF     STATUS, RP0     ; Select Bank 1 (RP1=0, RP0=1)
        BANK1
        BCF     TRISA, RA0      ; Clear TRISA bit 0 to make RA0 an output

        BCF     STATUS, RP0     ; Select Bank 0 again for PORTA access
        CLRF    PORTA           ; Ensure PORTA is initially low

        GOTO    MAIN_LOOP       ; Start main program loop

    ; --- Delay Subroutine (approx 500ms for 8MHz internal osc) ---
    ; This delay is an approximation. For 8MHz internal oscillator,
    ; 1 instruction cycle = 0.5 us. For 500ms, we need 1,000,000 cycles.
    ; This 3-nested loop provides approx 977,620 cycles.
    DELAY_500MS:
        MOVLW   0x0A            ; Load 10 into W (Outer loop count)
        MOVWF   DLY_OUTER       ; Move to DLY_OUTER
    OUTER_LOOP:
        MOVLW   0x82            ; Load 130 into W (Middle loop count)
        MOVWF   DLY_MIDDLE      ; Move to DLY_MIDDLE
    MIDDLE_LOOP:
        MOVLW   0xFA            ; Load 250 into W (Inner loop count)
        MOVWF   DLY_INNER       ; Move to DLY_INNER
    INNER_LOOP:
        NOP                     ; 1 cycle
        DECFSZ  DLY_INNER, F    ; 1 cycle (skip) or 2 cycles (no skip)
        GOTO    INNER_LOOP      ; 2 cycles
        DECFSZ  DLY_MIDDLE, F   ; 1 cycle (skip) or 2 cycles (no skip)
        GOTO    MIDDLE_LOOP     ; 2 cycles
        DECFSZ  DLY_OUTER, F    ; 1 cycle (skip) or 2 cycles (no skip)
        GOTO    OUTER_LOOP      ; 2 cycles
        RETURN                  ; 2 cycles

    ; --- RAM Variables ---
    DLY_OUTER   EQU     0x20    ; Delay loop counter 1
    DLY_MIDDLE  EQU     0x21    ; Delay loop counter 2
    DLY_INNER   EQU     0x22    ; Delay loop counter 3

    ; --- Bit Definitions (for clarity, usually in .inc files) ---
    RA0         EQU     0x00    ; PORTA bit 0
    RP0         EQU     0x05    ; STATUS register bit 5
    RP1         EQU     0x06    ; STATUS register bit 6

    END
