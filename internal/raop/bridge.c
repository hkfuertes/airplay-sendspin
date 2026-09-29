#include "bridge.h"

#include <arpa/inet.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>

#include "raop_server.h"
#include "cross_log.h"

log_level raop_loglevel = lINFO;
log_level util_loglevel = lWARN;

extern void goRaopPCM(uintptr_t owner, const int16_t *samples, size_t frames);
extern void goRaopEvent(uintptr_t owner, int event);
extern void goRaopVolume(uintptr_t owner, double volume);

struct bridge_receiver {
	struct raopsr_s *server;
	uintptr_t owner;
};

static void bridge_pcm(void *owner, const int16_t *samples, size_t frames) {
	bridge_receiver_t *receiver = owner;
	goRaopPCM(receiver->owner, samples, frames);
}

static void bridge_event(void *owner, raopsr_event_t event, ...) {
	bridge_receiver_t *receiver = owner;

	switch (event) {
	case RAOP_PLAY:
	case RAOP_FLUSH:
	case RAOP_STOP:
		goRaopEvent(receiver->owner, event);
		break;
	case RAOP_VOLUME: {
		va_list args;
		va_start(args, event);
		goRaopVolume(receiver->owner, va_arg(args, double));
		va_end(args);
		break;
	}
	default:
		break;
	}
}

bridge_receiver_t *bridge_receiver_new(const char *name, const uint8_t mac[6], const uint8_t ipv4[4],
										 uint16_t port_base, uint16_t port_range, uintptr_t owner) {
	struct in_addr host;
	char latency[] = "500";
	bridge_receiver_t *receiver;

	if (!name || !mac || !ipv4) return NULL;
	memcpy(&host.s_addr, ipv4, 4);
	receiver = calloc(1, sizeof(*receiver));
	if (!receiver) return NULL;

	receiver->owner = owner;
	receiver->server = raopsr_create(host, NULL, (char *) name, "AirPort10,115",
		(unsigned char *) mac, "wav", false, true, true, latency, receiver,
		bridge_event, NULL, port_base, port_range, 0);
	if (!receiver->server) {
		free(receiver);
		return NULL;
	}

	raopsr_set_pcm_callback(receiver->server, bridge_pcm, receiver);
	return receiver;
}

void bridge_receiver_delete(bridge_receiver_t *receiver) {
	if (!receiver) return;
	raopsr_delete(receiver->server);
	free(receiver);
}

uint16_t bridge_receiver_port(const bridge_receiver_t *receiver) {
	return receiver ? raopsr_port(receiver->server) : 0;
}
