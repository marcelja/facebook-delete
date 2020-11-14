email="EMAIL"
password="PASSWORD"

# this is expected to fail:
curl https://mbasic.facebook.com/login/device-based/regular/login/ \
  --data-raw "lsd=$lsd_token&jazoest=$jazoest_loken&li=$li_loken&login=Log+In" \
  --data-urlencode "email=$email" \
  --data-urlencode "pass=$password" \
  --compressed -b cookies.txt -c cookies.txt > response_login1.html

string=$(curl https://mbasic.facebook.com/ -b cookies.txt -c cookies.txt)
lsd_token=$(echo ${string#*lsd\" value=\"} | cut -d '"' -f1)
jazoest_token=$(echo ${string#*jazoest\" value=\"} | cut -d '"' -f1)
li_token=$(echo ${string#*li\" value=\"} | cut -d '"' -f1)

echo $lsd_token
echo $jazoest_token
echo $li_token

# this is expected to work:
curl https://mbasic.facebook.com/login/device-based/regular/login/ \
  --data-raw "lsd=$lsd_token&jazoest=$jazoest_loken&li=$li_loken&login=Log+In" \
  --data-urlencode "email=$email" \
  --data-urlencode "pass=$password" \
  --compressed -b cookies.txt -c cookies.txt > response_login2.html

string=$(curl https://mbasic.facebook.com/ -b cookies.txt -c cookies.txt)
echo "Login worked if user name is included:"
echo ${string: -200}
