insert into sample_items (name)
values ('hello from seed')
on conflict do nothing;
