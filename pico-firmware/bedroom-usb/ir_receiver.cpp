#include "ir_receiver.hpp"

#include "pico/stdlib.h"

const uint IR_RECEIVER_PIN = 14;

void ir_receiver_init() {
    gpio_init(IR_RECEIVER_PIN);
    gpio_set_dir(IR_RECEIVER_PIN, GPIO_IN);

    // OSRB38C9AAのOUTは通常HIGH、受信時LOWになる想定。
    // 入力が不安定にならないように内部プルアップを有効化する。
    gpio_pull_up(IR_RECEIVER_PIN);
}

bool ir_wait_for_signal_ms(unsigned int timeout_ms) {
    const absolute_time_t deadline = make_timeout_time_ms(timeout_ms);

    while (!time_reached(deadline)) {
        // 多くの赤外線受信モジュールは、受信時にLOWになる
        const bool detected = gpio_get(IR_RECEIVER_PIN) == 0;

        if (detected) {
            return true;
        }

        sleep_ms(1);
    }

    return false;
}

int ir_capture_signal(
    unsigned int* timings,
    int max_count,
    unsigned int timeout_ms,
    unsigned int gap_timeout_us
) {
    const absolute_time_t deadline = make_timeout_time_ms(timeout_ms);

    // 信号開始を待つ
    // OSRB38C9AAは未受信時HIGH、受信時LOWなので、最初のLOWを待つ
    while (!time_reached(deadline)) {
        if (gpio_get(IR_RECEIVER_PIN) == 0) {
            break;
        }

        sleep_us(50);
    }

    if (time_reached(deadline)) {
        return 0;
    }

    int count = 0;
    int current_level = gpio_get(IR_RECEIVER_PIN);
    uint32_t last_change_us = time_us_32();

    while (count < max_count) {
        int level = gpio_get(IR_RECEIVER_PIN);
        uint32_t now_us = time_us_32();

        if (level != current_level) {
            timings[count] = now_us - last_change_us;
            count++;

            current_level = level;
            last_change_us = now_us;
            continue;
        }

        // 信号終了判定
        // 受信モジュールがHIGHに戻ったまま一定時間経ったら終了とみなす
        if (current_level == 1 && (now_us - last_change_us) > gap_timeout_us) {
            break;
        }

        sleep_us(10);
    }

    return count;
}