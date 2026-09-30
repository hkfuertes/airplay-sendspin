#include "bridge.h"

#include <arpa/inet.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>

#include "cross_log.h"
#include "raop_server.h"

log_level raop_loglevel = lINFO;
log_level util_loglevel = lWARN;

// ponytail: fixed two-second buffer bounds latency; make it configurable only for sustained receiver stalls.
#define PCM_FRAMES (44100 * 2)
#define PCM_SAMPLES (PCM_FRAMES * 2)
#define EVENT_CAPACITY 32

typedef struct {
	int type;
	double volume;
} bridge_event_t;

struct bridge_receiver {
	struct raopsr_s *server;
	pthread_mutex_t lock;
	int16_t *pcm;
	size_t pcm_head;
	size_t pcm_size;
	bridge_event_t events[EVENT_CAPACITY];
	size_t event_head;
	size_t event_size;
};

static void clear_pcm(bridge_receiver_t *receiver) {
	receiver->pcm_head = 0;
	receiver->pcm_size = 0;
}

static void queue_event(bridge_receiver_t *receiver, int type, double volume) {
	size_t slot;
	if (receiver->event_size == EVENT_CAPACITY) {
		receiver->event_head = (receiver->event_head + 1) % EVENT_CAPACITY;
		receiver->event_size--;
	}
	slot = (receiver->event_head + receiver->event_size) % EVENT_CAPACITY;
	receiver->events[slot] = (bridge_event_t){ .type = type, .volume = volume };
	receiver->event_size++;
}

static void push_pcm(bridge_receiver_t *receiver, const int16_t *samples, size_t count) {
	size_t overflow, first;
	if (count >= PCM_SAMPLES) {
		samples += count - PCM_SAMPLES;
		count = PCM_SAMPLES;
		clear_pcm(receiver);
	}
	overflow = receiver->pcm_size + count > PCM_SAMPLES ? receiver->pcm_size + count - PCM_SAMPLES : 0;
	if (overflow) {
		receiver->pcm_head = (receiver->pcm_head + overflow) % PCM_SAMPLES;
		receiver->pcm_size -= overflow;
	}
	first = count < PCM_SAMPLES - ((receiver->pcm_head + receiver->pcm_size) % PCM_SAMPLES)
		? count : PCM_SAMPLES - ((receiver->pcm_head + receiver->pcm_size) % PCM_SAMPLES);
	memcpy(receiver->pcm + ((receiver->pcm_head + receiver->pcm_size) % PCM_SAMPLES), samples, first * sizeof(*samples));
	if (count > first) memcpy(receiver->pcm, samples + first, (count - first) * sizeof(*samples));
	receiver->pcm_size += count;
}

static void bridge_pcm(void *owner, const int16_t *samples, size_t frames) {
	bridge_receiver_t *receiver = owner;
	if (!receiver || !samples || !frames) return;
	pthread_mutex_lock(&receiver->lock);
	push_pcm(receiver, samples, frames * 2);
	pthread_mutex_unlock(&receiver->lock);
}

static void bridge_event(void *owner, raopsr_event_t event, ...) {
	bridge_receiver_t *receiver = owner;
	double volume;
	if (!receiver) return;
	pthread_mutex_lock(&receiver->lock);
	switch (event) {
	case RAOP_PLAY:
		clear_pcm(receiver);
		queue_event(receiver, BRIDGE_RAOP_PLAY, 0);
		break;
	case RAOP_FLUSH:
		clear_pcm(receiver);
		queue_event(receiver, BRIDGE_RAOP_FLUSH, 0);
		break;
	case RAOP_STOP:
		clear_pcm(receiver);
		queue_event(receiver, BRIDGE_RAOP_STOP, 0);
		break;
	case RAOP_VOLUME: {
		va_list args;
		va_start(args, event);
		volume = va_arg(args, double);
		va_end(args);
		queue_event(receiver, BRIDGE_RAOP_VOLUME, volume);
		break;
	}
	default:
		break;
	}
	pthread_mutex_unlock(&receiver->lock);
}

bridge_receiver_t *bridge_receiver_new(const char *name, const uint8_t mac[6], const uint8_t ipv4[4],
	uint16_t port_base, uint16_t port_range) {
	struct in_addr host;
	char latency[] = "500";
	bridge_receiver_t *receiver;
	if (!name || !mac || !ipv4) return NULL;
	memcpy(&host.s_addr, ipv4, 4);
	receiver = calloc(1, sizeof(*receiver));
	if (!receiver) return NULL;
	if (pthread_mutex_init(&receiver->lock, NULL)) {
		free(receiver);
		return NULL;
	}
	receiver->pcm = malloc(PCM_SAMPLES * sizeof(*receiver->pcm));
	if (!receiver->pcm) {
		pthread_mutex_destroy(&receiver->lock);
		free(receiver);
		return NULL;
	}
	receiver->server = raopsr_create(host, NULL, (char *)name, "AirPort10,115",
		(unsigned char *)mac, "wav", false, true, true, latency, receiver,
		bridge_event, NULL, port_base, port_range, 0);
	if (!receiver->server) {
		free(receiver->pcm);
		pthread_mutex_destroy(&receiver->lock);
		free(receiver);
		return NULL;
	}
	raopsr_set_pcm_callback(receiver->server, bridge_pcm, receiver);
	return receiver;
}

void bridge_receiver_delete(bridge_receiver_t *receiver) {
	if (!receiver) return;
	if (receiver->server) raopsr_delete(receiver->server);
	pthread_mutex_destroy(&receiver->lock);
	free(receiver->pcm);
	free(receiver);
}

uint16_t bridge_receiver_port(const bridge_receiver_t *receiver) {
	return receiver && receiver->server ? raopsr_port(receiver->server) : 0;
}

size_t bridge_receiver_read_pcm(bridge_receiver_t *receiver, int16_t *dst, size_t capacity_frames) {
	size_t count, first;
	if (!receiver || !dst || !capacity_frames) return 0;
	pthread_mutex_lock(&receiver->lock);
	count = receiver->pcm_size < capacity_frames * 2 ? receiver->pcm_size : capacity_frames * 2;
	count -= count % 2;
	first = count < PCM_SAMPLES - receiver->pcm_head ? count : PCM_SAMPLES - receiver->pcm_head;
	if (first) memcpy(dst, receiver->pcm + receiver->pcm_head, first * sizeof(*dst));
	if (count > first) memcpy(dst + first, receiver->pcm, (count - first) * sizeof(*dst));
	receiver->pcm_head = (receiver->pcm_head + count) % PCM_SAMPLES;
	receiver->pcm_size -= count;
	pthread_mutex_unlock(&receiver->lock);
	return count / 2;
}

int bridge_receiver_read_event(bridge_receiver_t *receiver, double *volume) {
	bridge_event_t event;
	if (!receiver) return 0;
	pthread_mutex_lock(&receiver->lock);
	if (!receiver->event_size) {
		pthread_mutex_unlock(&receiver->lock);
		return 0;
	}
	event = receiver->events[receiver->event_head];
	receiver->event_head = (receiver->event_head + 1) % EVENT_CAPACITY;
	receiver->event_size--;
	pthread_mutex_unlock(&receiver->lock);
	if (volume) *volume = event.volume;
	return event.type;
}
