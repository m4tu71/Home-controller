#pragma once

void ir_init();

void ir_mark_us(unsigned int duration_us);
void ir_space_us(unsigned int duration_us);

void ir_burst_ms(unsigned int duration_ms);
void ir_test_signal();
void ir_send_panasonic_40bit(unsigned long long data);

void ir_send_raw_timings(const unsigned int* timings, int count);