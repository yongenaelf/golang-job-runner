import os
from locust import HttpUser, TaskSet, task, between

class UserBehavior(TaskSet):

    @task
    def upload_file_build(self):
        # Path to a sample file for upload
        file_path = './src.zip'
        with open(file_path, 'rb') as f:
            # Make a POST request to the /upload endpoint
            self.client.post("/upload", files={"file": f})

    @task
    def upload_file_test(self):
        # Path to a sample file for upload
        file_path = './test.zip'
        with open(file_path, 'rb') as f:
            # Make a POST request to the /upload?command=test endpoint
            self.client.post("/upload?command=test", files={"file": f})

class WebsiteUser(HttpUser):
    tasks = [UserBehavior]
    # Wait time between tasks can be defined (e.g., between 1 and 5 seconds)
    wait_time = between(1, 5)

if __name__ == "__main__":
    # Starting point for the script if needed straight from the main execution
    import locust
    locust.run_single_user(WebsiteUser)