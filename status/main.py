# import all needed libriaries
# ...

async def monitor_downloads():
    async with aiohttp.ClientSession() as session:
        api = SynologyDSM(session, "192.168.1.34", "5000", "<username>", "<password>")
        await api.login()
        # Обновить данные Download Station
        await api.download_station.update()
        # Получить все задачи
        tasks = api.download_station.get_all_tasks()
        print(tasks)
        for task in tasks:
            print(f"📦 {task.title}")
            print(f"   Статус: {task.status}")
            print(f"   Размер: {task.size / 1024 / 1024 / 1024:.2f} GB")
            # print(f"   Прогресс: {task.size / task.size * 100:.1f}%")
            secs = task.additional["detail"]["completed_time"] - task.additional["detail"]["started_time"]
            print(f"   ⬇️ Downloaded:  {secs / 60 / 60 :.2f} hours")
            speed = task.size / secs
            print(f"   ⬇️ Average Speed: {speed / 1024 / 1024 :.2f} MB/s")