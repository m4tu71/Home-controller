#include "ir_sender.hpp"

#include "pico/stdlib.h"

const uint IR_PIN = 15;

// 38kHz: 1周期 約26.3us
// 半周期 約13us
const unsigned int IR_HALF_PERIOD_US = 13;

void ir_init() {
    gpio_init(IR_PIN);
    gpio_set_dir(IR_PIN, GPIO_OUT);
    gpio_put(IR_PIN, 0);
}

void ir_mark_us(unsigned int duration_us) {
    unsigned int elapsed_us = 0;

    while (elapsed_us < duration_us) {
        gpio_put(IR_PIN, 1);
        sleep_us(IR_HALF_PERIOD_US);

        gpio_put(IR_PIN, 0);
        sleep_us(IR_HALF_PERIOD_US);

        elapsed_us += IR_HALF_PERIOD_US * 2;
    }
    
}

void ir_space_us(unsigned int duration_us) {
    gpio_put(IR_PIN, 0);
    sleep_us(duration_us);
}

void ir_burst_ms(unsigned int duration_ms) {
    ir_mark_us(duration_ms * 1000);
}

void ir_test_signal() {
    ir_mark_us(9000);
    ir_space_us(4500);

    ir_mark_us(560);
    ir_space_us(560);

    ir_mark_us(560);
    ir_space_us(1690);

    ir_mark_us(560);
}

void ir_send_panasonic_40bit(unsigned long long data) {
    // Panasonic-like leader
    ir_mark_us(3500);
    ir_space_us(1750);

    // Send 40 bits, MSB first
    for (int i = 39; i >= 0; i--) {
        bool bit = (data >> i) & 1ULL;

        ir_mark_us(450);

        if (bit) {
            ir_space_us(1300);
        } else {
            ir_space_us(450);
        }
    }

    // Stop bit
    ir_mark_us(450);
    ir_space_us(10000);
}

void ir_send_raw_timings(const unsigned int* timings, int count) {
    for (int i = 0; i < count; i++) {
        if (i % 2 == 0) {
            ir_mark_us(timings[i]);
        } else {
            ir_space_us(timings[i]);
        }
    }
}