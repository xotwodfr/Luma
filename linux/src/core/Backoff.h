#pragma once

#include <algorithm>
#include <chrono>
#include <cstdint>
#include <functional>
#include <random>

// Exponential backoff with full jitter: delay ∈ [0, min(30 s, 0.5 s · 2^attempt)] (protocol/realtime.md).
class Backoff {
public:
    using Rng = std::function<double()>; // uniform in [0, 1)

    explicit Backoff(Rng rng = {})
        : m_rng(rng ? std::move(rng) : defaultRng())
    {
    }

    std::chrono::milliseconds next()
    {
        const double cap = std::min(30000.0, 500.0 * double(1u << std::min(m_attempt, 16)));
        m_attempt = std::min(m_attempt + 1, 16);
        return std::chrono::milliseconds(std::int64_t(m_rng() * cap));
    }

    void reset() { m_attempt = 0; }

private:
    static Rng defaultRng()
    {
        return [gen = std::mt19937(std::random_device{}()), dist = std::uniform_real_distribution<double>(0.0, 1.0)]() mutable {
            return dist(gen);
        };
    }

    Rng m_rng;
    int m_attempt = 0;
};
