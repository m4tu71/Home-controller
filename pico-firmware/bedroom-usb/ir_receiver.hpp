#pragma once

void ir_receiver_init();
bool ir_wait_for_signal_ms(unsigned int timeout_ms);

int ir_capture_signal(
    unsigned int* timings,
    int max_count,
    unsigned int timeout_ms,
    unsigned int gap_timeout_us
);