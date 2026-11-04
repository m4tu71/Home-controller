#include "led_controller.hpp"

#include "pico/stdlib.h"

const uint LED_PIN = 25;

static bool led_state = false;

void led_init() {
    gpio_init(LED_PIN);
    gpio_set_dir(LED_PIN, GPIO_OUT);
    gpio_put(LED_PIN, 0);

  led_state = false;
}

void led_on() {
    gpio_put(LED_PIN, 1);
    led_state = true;
}

void led_off() {
    gpio_put(LED_PIN, 0);
    led_state = false;
}

bool led_is_on() {
    return led_state;
}