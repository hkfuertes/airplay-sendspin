#pragma once

#include <stddef.h>
#include <stdint.h>

typedef struct bridge_receiver bridge_receiver_t;

enum {
	BRIDGE_RAOP_PLAY = 1,
	BRIDGE_RAOP_FLUSH = 2,
	BRIDGE_RAOP_STOP = 4,
	BRIDGE_RAOP_VOLUME = 8,
};

bridge_receiver_t *bridge_receiver_new(const char *name, const uint8_t mac[6], const uint8_t ipv4[4],
	uint16_t port_base, uint16_t port_range);
void bridge_receiver_delete(bridge_receiver_t *receiver);
uint16_t bridge_receiver_port(const bridge_receiver_t *receiver);
size_t bridge_receiver_read_pcm(bridge_receiver_t *receiver, int16_t *dst, size_t capacity_frames);
int bridge_receiver_read_event(bridge_receiver_t *receiver, double *volume);
