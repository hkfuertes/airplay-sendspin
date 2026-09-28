#pragma once

#include <stddef.h>
#include <stdint.h>

typedef struct bridge_receiver bridge_receiver_t;

bridge_receiver_t *bridge_receiver_new(const char *name, const uint8_t mac[6], const uint8_t host[4],
                                       uint16_t port_base, uint16_t port_range,
                                       uintptr_t owner);
void bridge_receiver_delete(bridge_receiver_t *receiver);
uint16_t bridge_receiver_port(const bridge_receiver_t *receiver);

enum {
	BRIDGE_RAOP_PLAY = 1,
	BRIDGE_RAOP_FLUSH = 2,
	BRIDGE_RAOP_STOP = 4,
};
