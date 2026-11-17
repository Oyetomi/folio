---
id: "[1.1.0]"
title: "Queues and Waiting"
---

# Queues and Waiting

A queue forms whenever work arrives faster than it can be served, even briefly. Requests at a web server, packets at a router and customers at a bakery all behave the same way once you describe them with three numbers: how often work arrives, how long it takes to serve, and how many servers there are.

- **Arrival rate:** The average number of jobs arriving per unit of time, written $\lambda$.
- **Service rate:** The average number of jobs one server can finish per unit of time, written $\mu$.
- **Utilization:** The fraction of time the server is busy, $\rho = \lambda / \mu$.

## Little's Law

The most useful result in queueing theory is also the simplest. For any stable system, the average number of jobs inside it equals the arrival rate multiplied by the average time each job spends there:

$$
L = \lambda W
$$

It holds regardless of the arrival pattern, the service-time distribution or the scheduling order. If a service handles 200 requests per second and each request spends 50 ms inside it, then on average $200 \times 0.05 = 10$ requests are in flight.

> [!NOTE]
> Little's law needs only that the system is stable: over a long enough window, as many jobs leave as arrive.

## The M/M/1 Queue

When arrivals are random (a Poisson process), service times are exponentially distributed and there is a single server, the average time a job spends in the system is

$$
W = \frac{1}{\mu - \lambda} = \frac{1}{\mu (1 - \rho)}
$$

The factor $1/(1-\rho)$ is the important part. Waiting time does not grow in proportion to load; it grows without bound as utilization approaches one.

| Utilization $\rho$ | Time in system (multiples of service time) | Jobs in system |
|:-------------------|-------------------------------------------:|---------------:|
| 0.50               | 2.0                                        | 1.0            |
| 0.80               | 5.0                                        | 4.0            |
| 0.90               | 10.0                                       | 9.0            |
| 0.95               | 20.0                                       | 19.0           |
| 0.99               | 100.0                                      | 99.0           |

Going from 80% to 90% utilization doubles the waiting time. Going from 90% to 99% multiplies it by ten. This is why systems that look fine at average load fall over during short bursts.

> [!WARNING]
> Capacity plans based on average utilization hide this curve. Plan for the peaks, and keep headroom.
