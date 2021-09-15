from b2sdk.v2 import *
import os
import requests



info = InMemoryAccountInfo()
b2_api = B2Api(info)
application_key_id = os.environ.get('B2_APP_KEY_ID')
application_key = os.environ.get('B2_APP_KEY')
b2_api.authorize_account('production', application_key_id, application_key)

package_path = os.path.join(os.getcwd(), './master.zip')

with requests.get('https://github.com/rehmatworks/fastcp/archive/refs/heads/master.zip') as resp:
    with open(package_path, 'wb') as f:
        f.write(resp.content)

bucket = b2_api.get_bucket_by_name('fastcp')
bucket.upload_local_file(
    local_file=package_path,
    file_name='latest.zip'
)